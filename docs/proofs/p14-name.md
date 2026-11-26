# Point 14: is the name `rollproof` free to take?

## Question

Does another GitHub account, repository, action, package or image called `rollproof` do work like
the tool's, is the name taken where a release would publish to it, and is a trademark for software
registered under it (section 14, point 14)?

This proof has no program and no cluster. It is a search.

## Machine and versions

The maintainer's definition of a collision, chosen on 2026-11-24: something that would block or
seriously mislead. That is (1) a GitHub repository, action, package or image called `rollproof`,
`roll-proof` or `roll_proof` that does similar work (Kubernetes, CI, release automation, deploy or
rollout testing); (2) a package or an account called `rollproof` where the name is global and a
release could publish (GitHub users and organizations, Homebrew); (3) a trademark for software,
the nice classes 9 and 42. Any other use of the word is noted and does not block.

Read on 2026-11-24, between 15:14 and 15:31 UTC, by AI agents on the session's default model
(GLM-5.4): two background agents fetched answers from 15:14 to 15:21 UTC and then stopped at the
model service's rate limit, each before writing a note; the session read their saved answers from
15:23 UTC on and made the rest of the requests itself. Tools: `gh` 2.103.3 for GitHub's API, GET
requests only; Python 3.14's `urllib` for the other APIs, GET only but one Flathub search; the
session's web search tool, which does not show which engine it asks. The raw answers are not filed
in the repository. The five searches of the trademark databases were made by the maintainer in a
browser on 2026-11-26.

## Method

Each request is listed under Result with its exact address. Three rules were kept: a finding comes
only from a page or an API that was read, never from memory; a search engine's page is marked
`secondary`; a source that could not be read says so with the reason.

## Result

### GitHub

| Request | Answer | Finding |
| --- | --- | --- |
| `gh api search/repositories q='rollproof in:name'` (15:14 and 15:31 UTC) | `total_count` 1: `Javanian/rollproof`, created 2026-10-14, pushed 2026-11-22, 0 stars, 1 release, no licence file. README: "Attendance with proof. Employees check in from their phone with a face match and a GPS fix, even without signal." Python, React and Expo. | noted |
| the same with `roll-proof in:name` and with `roll_proof in:name` | `total_count` 11 each, the same eleven repositories. Ten are unrelated. The eleventh is `ch0992/rollout-proof` (below). | noted |
| `gh api repos/ch0992/rollout-proof` (15:24 UTC) | description "Evidence-based Kubernetes rollout continuity verification CLI"; public; created 2026-08-18, pushed 2026-08-19; 36 commits; 0 stars, 0 forks, 40 open issues; no release, no tag, no licence file; Shell, Makefile, Go (578 bytes), Ruby. Its README (Korean) describes one Go binary, `rollout-proof gate deployment/api`, that waits for a Deployment's new revision while an HTTP probe checks the Service, a kind E2E test, a container and a GitHub Action; its specs are at 0.1-draft. Its ADR-0001 of 2026-08-18 fixes the name `RolloutProof`. | noted; a finding for the maintainer (Decision) |
| `gh api users/rollproof`, `users/roll-proof`, `users/roll_proof` (15:15 UTC) | 404 for each | none |
| `gh api orgs/rollproof`, `orgs/roll-proof`, `orgs/roll_proof` (15:15 UTC) | 404 for each | none |
| `gh api search/users q=rollproof` | `total_count` 0 | none |
| `gh api repos/nzneit/rollproof` | 404: the name is not taken under the maintainer's account | none |
| `gh api search/repositories q='topic:rollproof'` | `total_count` 0 | none |
| `gh api search/code q='rollproof path:.github/workflows'` and a second wording of it (15:16 UTC) | `total_count` 5, all five in the maintainer's own private repository; no other workflow calls an action of that name | none |
| `https://github.com/marketplace?type=actions&query=rollproof` (15:19 and 15:31 UTC) | the page holds no link to an action. Control: the same page with `query=checkout` holds 20 such links, so the page lists results without a script. | none |

### Go, containers, the Kubernetes ecosystem

| Request | Answer | Finding |
| --- | --- | --- |
| `https://pkg.go.dev/search?q=rollproof` and `?q=roll-proof` (15:19 UTC) | each page's HTML holds no link to a module or a package | none |
| `https://hub.docker.com/v2/search/repositories/?query=rollproof&page_size=100` (15:20 UTC) | `count` 0 | none |
| `https://hub.docker.com/v2/repositories/rollproof/?page_size=100` (15:20 and 15:26 UTC) | `count` 0: no namespace of that name holds an image | none |
| `https://hub.docker.com/v2/namespaces/rollproof/` | 405 | not read |
| `https://hub.docker.com/v2/search/repositories/?query=roll-proof` | `count` 49,606: the search is fuzzy and its first 40 names are unrelated | not read: an image called `roll-proof` under some namespace cannot be excluded by this search |
| `https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry` (15:26 UTC) | "docker push ghcr.io/NAMESPACE/IMAGE_NAME:latest Replace NAMESPACE with the name of the personal account or organization to which you want the image to be scoped." | none: an image's name there lives under an owner |
| `https://artifacthub.io/api/v1/packages/search?ts_query=rollproof` | `packages` 0 | none |
| `https://raw.githubusercontent.com/kubernetes-sigs/krew-index/master/plugins/rollproof.yaml` | 404: no kubectl plugin of that name | none |

### Package registries

| Request | Answer | Finding |
| --- | --- | --- |
| `https://pypi.org/pypi/rollproof/json` and the same for `roll-proof` | 404 | none |
| `https://registry.npmjs.org/-/v1/search?text=rollproof&size=100` | `total` 0 | none |
| `https://crates.io/api/v1/crates?q=rollproof&per_page=100` | `meta.total` 0 | none |
| `https://rubygems.org/api/v1/search.json?query=rollproof` | an empty list | none |
| `https://search.maven.org/solrsearch/select?q=rollproof&rows=50&wt=json` | `numFound` 0 | none |
| `https://azure-search-usnc.nuget.org/query?q=rollproof` | the host's name did not resolve from the machine | not read |
| `https://www.nuget.org/api/v2/Packages()?$filter=Id eq 'rollproof'`, and the same for `roll-proof` (15:27 UTC) | a feed with no entry, for each | none |
| `https://packagist.org/search.json?q=rollproof&per_page=50` | 4 fuzzy results (`rollproject/…`, a "fullproof mvc generator"); none of the name | none |

All of these were read between 15:26 and 15:28 UTC.

### Operating-system package indexes

| Request | Answer | Finding |
| --- | --- | --- |
| `https://formulae.brew.sh/api/formula/rollproof.json`, `…/cask/rollproof.json`, and both for `roll-proof` | 404 for each | none |
| `https://sources.debian.org/api/search/rollproof/` | `{"exact": null, "other": []}` | none |
| `https://packages.ubuntu.com/search?keywords=rollproof&searchon=names&suite=all&section=all` | the page's sentence that the search gave no results | none |
| `https://archlinux.org/packages/search/json/?q=rollproof` | `results` 0 | none |
| `https://aur.archlinux.org/rpc/v5/search/rollproof?by=name` | `resultcount` 0 | none |
| `https://snapcraft.io/search?q=rollproof` | the page's HTML does not contain the word; the search runs by script | not read |
| `https://flathub.org/api/v2/search` with `{"query":"rollproof","filters":[]}` | 16 fuzzy hits (`Meine Ausweiskopie`, `SiYuan`, `Kopia`, …); none of the name | none |
| `https://search.nixos.org/packages?…query=rollproof…` | an HTML page that lists nothing without a script | not read |
| `gh api search/code q='rollproof repo:NixOS/nixpkgs'` (15:29 UTC) | `total_count` 0 | none, for nixpkgs' default branch |
| on the developer's machine (CachyOS, which uses pacman): `command -v rollproof roll-proof` and `pacman -Ss rollproof` | no output | none, for that machine's own package lists only |

### Another forge, stores

| Request | Answer | Finding |
| --- | --- | --- |
| `https://gitlab.com/api/v4/projects?search=rollproof&per_page=50` | 0 projects | none |
| `https://itunes.apple.com/search?term=rollproof&entity=software&limit=50` (15:18 UTC) | `resultCount` 3: `Roll Weight`, `Crazy Rolly`, `Lucky Packaging` | none |
| `https://play.google.com/store/search?q=rollproof&c=apps` (15:18 UTC) | 8 apps (`com.azenlabs.rollr`, `com.glennfrancis.rollz`, `com.invenzen.lynkr`, `com.rollout.app`, …); none of the name | none |
| `https://addons.mozilla.org/api/v5/addons/search/?q=rollproof` | 1 result, `RickRollProtector` | none |
| the VS Code Marketplace's `extensionquery` for the word (15:18 UTC) | `TotalCount` 0 | none |

### Domains, by RDAP (15:26 UTC)

| Domain | Answer | Finding |
| --- | --- | --- |
| `rollproof.com` | registered 2026-05-28, expires 2027-05-28, registrar Tucows Domains Inc. | noted |
| `rollproof.dev`, `rollproof.app` | 404, "rollproof.dev not found", "rollproof.app not found" | none |
| `rollproof.ai`, `rollproof.org` | 404, "Object not found" | none |
| `rollproof.net` | 404 with an empty body | none |
| `rollproof.io`, `rollproof.sh`, `rollproof.co` | "No RDAP service is available for this resource" | not read |

### The general web (secondary: the session's search tool, 15:27 to 15:31 UTC)

Queries: `"rollproof"`; `"rollproof" kubernetes OR "github action" OR CI/CD OR deployment`;
`"rollout-proof" kubernetes rollout continuity verification CLI`; `"rollproof" trademark OR company OR
software`; `"roll-proof" software OR tool OR app OR plugin`; `"roll proof" kubernetes OR rollout OR
deploy OR release testing tool`; `"rollproof" app OR software OR developer OR devops`.

- Every hit for the word is one of two things. A shop, `https://rollproof.com`: its pages (15:15
  UTC) call it "Wheelchair transfer car protection | RollProof" and "Better solutions for
  wheelchair users.", and its terms of service say "…your purchase of any products from ROLLPROOF
  LLC, a California limited liability company". It has accounts `@rollproof` on Instagram and
  Facebook. The word as an adjective on pens, grips and handles ("roll-proof handle"). **noted**
- The query on `rollout-proof` returned "No search results found". The repository above was found
  through GitHub's own search, not through the search tool.

### Trademarks and companies

| Request | Answer | Finding |
| --- | --- | --- |
| TMview, `https://www.tmdn.org/tmview/api/search/results` (a search POST, 15:15 UTC), twice | "Request Rejected … The requested URL was rejected" | not read |
| EUIPO eSearch, `https://euipo.europa.eu/eSearch/` | the application's shell, which holds no data without a script | not read |
| WIPO Global Brand Database, `https://branddb.wipo.int/` | a page that loads a captcha | not read |
| USPTO, `https://tmsearch.uspto.gov/` | the application's shell; its script names no address that answers a search | not read |
| United Kingdom IPO, `https://trademarks.ipo.gov.uk/`, by three routes | "Service Captcha" each time | not read |
| `https://trademarks.justia.com/search?q=rollproof` (secondary, 15:28 UTC) | 403, a challenge page | not read |
| OpenCorporates, `https://api.opencorporates.com/v0.4/companies/search?q=rollproof` (15:17 UTC) | "Invalid Api Token" | not read |

The maintainer searched the five databases in a browser on 2026-11-26, for the word `rollproof`:

| Database | What the search showed | Finding |
| --- | --- | --- |
| TMview | Two marks, each with the status `Ended` and the office of origin "United States - USPTO". One: "039 - Men's trousers", the applicant "EMBU INVESTMENTS SP. Z O.O., BRANCH IN LUXEMBOURG". The other: `ROLLPRUF`, number 71649051, the applicant "PIONEER RUBBER COMPANY, THE", "044 - Surgeons' gloves made from rubber or rubber-like material". The report gives "Trade Marks: 13976490 (updated 25/11/2026)" with each. | noted |
| EUIPO's eSearch | No results for "rollproof". | none |
| WIPO's Global Brand Database | `ROLLPROOF`: nice class 39, an "IPRNational Trademark Application" whose country of filing is the USA, with the status "Ended (June 1, 2012)", number 72305887. | noted |
| The USPTO's search | Two wordmarks. `ROLLPROOF`: "Dead Cancelled"; "US 039: [ MEN'S TROUSERS ]"; class 025; serial 72305887; owner "EMBU INVESTMENTS SP. Z O.O., BRANCH IN LUXEMBOURG (LIMITED LIABILITY COMPANY; POLAND)". `ROLLPRUF`: "Dead"; "US 044: [ SURGEONS' GLOVES MADE FROM RUBBER OR RUBBER-LIKE MATERIAL ]"; class 010; serial 71649051; owner "PIONEER RUBBER COMPANY, THE (CORPORATION; OHIO, USA)". | noted |
| The United Kingdom's | No results for "rollproof". | none |

## Decision

- **The name can be taken.** No GitHub user or organization is called `rollproof`, `nzneit/rollproof`
  does not exist, no action, package, image, Helm chart or kubectl plugin of that name was found,
  and Homebrew has no formula or cask of it.
- **Noted, and no block:** `Javanian/rollproof` has held the same name since 2026-10-14 for an
  attendance app, so a search for the word on GitHub shows two repositories; and ROLLPROOF LLC of
  California sells a car protector for wheelchair users at `rollproof.com`.
- **A finding for the maintainer, outside the name:** `ch0992/rollout-proof` is a project for the
  tool's own purpose, public since 2026-08-18, with no release. The search for prior art of
  2026-11-13 (section 3) did not find it. Section 14, point 15, says that finding an existing tool
  changes the project's reason to exist, not its design. **The maintainer ruled on 2026-11-24: the
  name `rollproof` stays.** `rollout-proof` is another name, and the design's paragraph on prior
  art should name the project.
- **The trademark half is closed.** The maintainer searched the five databases in a browser on
  2026-11-26, before anything was pushed, and found no mark for software in the nice classes 9 and
  42. What each showed is above. Two marks outside those classes were noted: `ROLLPROOF`, dead and
  cancelled, for men's trousers, and `ROLLPRUF`, dead, for surgeons' gloves.
- **Not searched at all:** Bitbucket, Codeberg, Gitea and SourceForge; a companies register; app
  stores other than the United States'; and anything a search engine does not index.

Plan assumed: "On GitHub no user and no organization is called `rollproof`, and `nzneit/rollproof` does not exist, so the account's name and the repository's name can be taken."
Holds: yes

Plan assumed: "No GitHub repository, action, package or image called `rollproof`, `roll-proof` or `roll_proof` does work like the tool's, and Homebrew has no formula or cask of the name."
Holds: yes

Plan assumed: "No trademark for software, in the nice classes 9 and 42, called `rollproof` is registered with the offices of the United States, the European Union, the United Kingdom or under the Madrid system."
Holds: yes
