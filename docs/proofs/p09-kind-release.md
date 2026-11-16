# Point 9: which kind release the tool links

## Question

Point 9 of section 14: which release of kind the tool links, and which node images kind lists for that release. The tool promises those of them that its own suite ran on (section 12).

## Machine and versions

The developer's machine, on 2026-11-16: Linux, amd64; git 2.56.0; curl 8.23.0. Nothing in this proof ran a container.

## Method

Three readings, made on 2026-11-16. No GitHub credential was sent: git ran with its credential helper turned off, and curl sent no token. The brief named `gh release view` for the third reading; it was not used, because a `gh` that is logged in would have sent its token.

1. The tags of the kind repository:

   ```
   git -c credential.helper= ls-remote --tags --refs https://github.com/kubernetes-sigs/kind
   ```

2. In a clone of kubernetes-sigs/kind, whose tag `v0.33.0` is the same tag object as the remote's (`49aeee6b958d818ae881752fe5b09220b39b6f55`, commit `407a9675e6d9af1200b5f57f9ca52ec6cdacce74`):

   ```
   git show v0.33.0:pkg/apis/config/defaults/image.go
   ```

3. The release notes, from GitHub's public releases API with no token, saved to a file (status 200); the notes are the `body` field of the answer:

   ```
   curl -sS https://api.github.com/repos/kubernetes-sigs/kind/releases/tags/v0.33.0
   ```

## Result

The newest tags, of 62 that `ls-remote` listed:

```
9a205e8c8540557602240f8766d3c95c51c23c4c	refs/tags/v0.30.0
f88da3b9c3618169ca7b0552b6af3366341c7332	refs/tags/v0.30.0-alpha
a323333ff9efd8099f95a8a6b5c86c75a210d00f	refs/tags/v0.31.0
4fcdc4994f53eedda078b0258a4d8f696d153f86	refs/tags/v0.31.0-alpha
7f833477ffb28ae3ecbf5ea23e91300db19bb047	refs/tags/v0.32.0
5c5fadc419aadd291423cc3d1da4882cdd322d59	refs/tags/v0.32.0-alpha
49aeee6b958d818ae881752fe5b09220b39b6f55	refs/tags/v0.33.0
3f9ba7e259e03b949c22b0fc1080de314402133a	refs/tags/v0.33.0-alpha
440e1abb2bdc2420916875640543f742eff90d26	refs/tags/v0.34.0-alpha
```

The newest tag that does not end in `-alpha` is `v0.33.0`. `v0.34.0-alpha` is listed. Apart from `vX.Y.Z` and `vX.Y.Z-alpha`, the only tags are four old ones without a `v` (`0.0.1`, `0.1.0`, `0.2.0`, `0.2.1`).

The line of `image.go` at `v0.33.0`:

```
const Image = "kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5"
```

The release `v0.33.0` was published on 2026-08-26 and is neither a draft nor a prerelease. Its notes say "defaults to Kubernetes 1.37.0" and list these node images, copied as given:

```
Images pre-built for this release:

* v1.37.0: `kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5`
* v1.36.4: `kindest/node:v1.36.4@sha256:099e049362a1526b2db71494e1947aae99bd16290d7c895f2b7ea312e3cbfaed`
* v1.35.8: `kindest/node:v1.35.8@sha256:07b2536e30b803ed61d1677a79df6115f798ce64c80f9e22f6ed45afd09323c0`
* v1.34.11: `kindest/node:v1.34.11@sha256:44e222ee2132dab25ff87301682f89eb82c7880ea3a1bf543bfe9708fd08d67d`
```

The notes add that the `@sha256` digest must be used to get an image built for this release, and that the images support amd64 and arm64.

## Decision

The tool links `sigs.k8s.io/kind` v0.33.0. It is the newest release that is not an alpha on 2026-11-16, and its default node image, `kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5`, is the newest of the four images it lists. The proofs module already links it.

The oldest image of the list, `kindest/node:v1.34.11@sha256:44e222ee2132dab25ff87301682f89eb82c7880ea3a1bf543bfe9708fd08d67d`, is the second image of the suite's matrix (section 12): the first fixture alone also runs on it. The two images between them, v1.36.4 and v1.35.8, are expected to work and are not promised (section 9).

Plan assumed: "The tool links sigs.k8s.io/kind v0.33.0, the newest release that is not an alpha on 2026-11-16; its default node image is kindest/node:v1.37.0."
Holds: yes
