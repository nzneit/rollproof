#!/usr/bin/env bash
# action.sh is the script of rollproof's composite action (action.yml). It checks that the runner
# is Linux, finds the container engine by the tool's own rule, pulls the tool's image by its
# digest, takes the binary out of it, and starts it with exec, in the workspace, so that the tool
# is the step's first process and gets the runner's signals (design, sections 8 and 14, point 20).
# It never writes an output: when it fails itself, it says why on standard error and exits 2, and
# the step's verdict stays empty. Sourced, it only defines its functions.
#
# Its input is the environment that action.yml's step gives it:
#   ROLLPROOF_ACTION_CONFIG      the input config
#   ROLLPROOF_ACTION_IMAGE       the input image
#   ROLLPROOF_ACTION_OUT         the input out
#   ROLLPROOF_ACTION_VERSION     written at a release; nothing reads it
#   ROLLPROOF_ACTION_REPOSITORY  written at a release: the image's repository
#   ROLLPROOF_ACTION_DIGEST      written at a release: the image's digest, sha256:…
# Besides bash and the engine, it uses uname, mktemp and tar.

set -euo pipefail
shopt -s execfail

# fail prints its message, after "rollproof: ", to standard error, and exits 2.
fail() {
  printf 'rollproof: %s\n' "$1" >&2
  exit 2
}

# pick_engine prints the engine's command name as the tool's pickEngine chooses it
# (internal/cluster/runner.go): KIND_EXPERIMENTAL_PROVIDER when it is one of the names that kind's
# command line takes, else the first of docker, nerdctl and podman whose -v exits 0 with a first
# line that begins with its banner. It returns 1 when there is none.
pick_engine() {
  case "${KIND_EXPERIMENTAL_PROVIDER-}" in
    docker | podman | nerdctl | finch | nerdctl.lima)
      printf '%s\n' "$KIND_EXPERIMENTAL_PROVIDER"
      return 0
      ;;
  esac
  local e banner out
  for e in docker nerdctl podman; do
    case $e in
      docker) banner='Docker version' ;;
      nerdctl) banner='nerdctl version' ;;
      podman) banner='podman version' ;;
    esac
    out=$("$e" -v 2>/dev/null) || continue
    case "${out%%$'\n'*}" in
      "$banner"*)
        printf '%s\n' "$e"
        return 0
        ;;
    esac
  done
  return 1
}

# image_ref prints the reference to pull: the input image when it holds @sha256:, else the input
# image, or the repository written at the release when the input is empty, with @ and the digest
# written at the release. Before the first release neither is written, and it fails.
image_ref() {
  local image=${ROLLPROOF_ACTION_IMAGE:-${ROLLPROOF_ACTION_REPOSITORY:-}}
  if [[ -z $image ]]; then
    fail "no image to pull: before its first release this action carries no repository and no digest, so give the input image a reference with @sha256:"
  fi
  case $image in
    *@sha256:*)
      printf '%s\n' "$image"
      ;;
    *)
      if [[ -z ${ROLLPROOF_ACTION_DIGEST:-} ]]; then
        fail "the image $image carries no digest, and before its first release this action carries none: give the input image a reference with @sha256:"
      fi
      printf '%s\n' "$image@$ROLLPROOF_ACTION_DIGEST"
      ;;
  esac
}

# take_binary takes /rollproof out of the image ref into the folder dir through the engine: it
# creates a container that never starts, with /rollproof as its command for an engine that wants
# one, exports its file system as a tar, keeps the one file rollproof, and removes the container.
# The route is export and tar because rootless nerdctl refuses cp from a container that never ran.
take_binary() {
  local engine=$1 ref=$2 dir=$3 id
  id=$("$engine" create "$ref" /rollproof) || fail "$engine could not create a container from $ref"
  if ! "$engine" export "$id" | tar -x -C "$dir" -f - rollproof; then
    "$engine" rm "$id" >/dev/null 2>&1 || true
    fail "the binary /rollproof could not be taken out of $ref"
  fi
  "$engine" rm "$id" >/dev/null 2>&1 || true
  [[ -x $dir/rollproof ]] || fail "the binary /rollproof could not be taken out of $ref"
}

# main runs the action: Linux, the engine, the image, the binary, and then the tool in place of
# this script, in the workspace, with KIND_EXPERIMENTAL_PROVIDER set to the engine it found and
# its own variables unset.
main() {
  local system engine ref dir temp=${RUNNER_TEMP:-${TMPDIR:-/tmp}}
  system=$(uname -s)
  [[ $system == Linux ]] || fail "the runner is not Linux (uname -s printed $system); the action runs only on Linux"
  engine=$(pick_engine) || fail "no container engine that kind supports: KIND_EXPERIMENTAL_PROVIDER names none, and none of docker, nerdctl and podman answers -v"
  ref=$(image_ref) || exit 2
  "$engine" pull "$ref" >&2 || fail "$engine could not pull $ref"
  dir=$(mktemp -d "$temp/rollproof-action-XXXXXX") || fail "no folder for the binary under $temp"
  take_binary "$engine" "$ref" "$dir"
  local args=(run -f "${ROLLPROOF_ACTION_CONFIG:-rollproof.yaml}")
  if [[ -n ${ROLLPROOF_ACTION_OUT:-} ]]; then
    args+=(--out "$ROLLPROOF_ACTION_OUT")
  fi
  cd "${GITHUB_WORKSPACE:-.}" || fail "the workspace ${GITHUB_WORKSPACE:-.} cannot be entered"
  printf 'rollproof: the tool from %s, through %s\n' "$ref" "$engine" >&2
  unset ROLLPROOF_ACTION_CONFIG ROLLPROOF_ACTION_IMAGE ROLLPROOF_ACTION_OUT \
    ROLLPROOF_ACTION_VERSION ROLLPROOF_ACTION_REPOSITORY ROLLPROOF_ACTION_DIGEST
  export KIND_EXPERIMENTAL_PROVIDER=$engine
  # With set -e on, bash itself ends the script at most execs that fail, also before ||, with its
  # own code. With it off, execfail lets a failed exec return, and the next line fails as the
  # other failures do.
  set +e
  exec "$dir/rollproof" "${args[@]}"
  fail "the binary taken out of $ref could not be started"
}

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
  main
fi
