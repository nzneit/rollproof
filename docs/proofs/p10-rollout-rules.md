# Point 10: the rollout rules

## Question

Point 10 of section 14: can kubectl's package with the rules of rollout status (`k8s.io/kubectl/pkg/polymorphichelpers`) be imported into the tool, what does it cost in binary size, what does each rule return for the cases the tool meets, and does the tool import the rules or copy them?

## Machine and versions

- System: the developer's machine, Linux 7.3.3, amd64.
- Go 1.27.3.
- `k8s.io/kubectl` v0.37.2, resolved by `go get k8s.io/kubectl@v0.37`.
- `k8s.io/client-go`, `k8s.io/api` and `k8s.io/apimachinery` v0.37.2.
- No cluster, no container engine and no kind in this proof.
- Source read: `k8s.io/kubectl@v0.37.2/pkg/polymorphichelpers/rollout_status.go` from the module cache. It is byte for byte the file at `staging/src/k8s.io/kubectl/pkg/polymorphichelpers/rollout_status.go` in Kubernetes at the tag v1.37.0 (`cmp` of the two files: identical).

## Method

The programs are under `proofs/p10-rollout-rules/`, in the module `rollproof-proofs`. All commands run from `projects/rollproof/proofs/`.

1. `go list -m all | wc -l` before the change, then `go get k8s.io/kubectl@v0.37`, then `go list -m k8s.io/kubectl k8s.io/client-go k8s.io/api k8s.io/apimachinery`, then `go mod tidy` and `go list -m all | wc -l` again.
2. `rules_test.go` has one test, `TestRolloutRules`, with a subtest for each of fifteen cases, a to o. A helper converts a typed `appsv1` object with `runtime.DefaultUnstructuredConverter.ToUnstructured`, takes the viewer from `polymorphichelpers.StatusViewerFor`, and calls `Status(u, 0)`, as `kubectl rollout status` does with revision 0. Each case logs message, done and error, and asserts only what the plan said to assert. The base objects are a Deployment `orders` (generation 2, 3 replicas, all counts 3), a StatefulSet `db` (generation 2, 3 replicas, RollingUpdate with partition 0, revisions `db-2`) and a DaemonSet `agent` (generation 2, RollingUpdate, 3 desired, updated, ready and available).
3. Two programs, `size/clientonly` and `size/withkubectl`, build a client-go clientset; the second also asks kubectl's package for the Deployment viewer. Both are built static and stripped, and their sizes compared.

```
go test ./p10-rollout-rules/ -v
env CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/clientonly ./p10-rollout-rules/size/clientonly
env CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/withkubectl ./p10-rollout-rules/size/withkubectl
stat -c "%s %n" bin/clientonly bin/withkubectl
```

## Result

**The package resolves with the client-go already required.** `go get k8s.io/kubectl@v0.37` added `k8s.io/kubectl v0.37.2` and changed none of the three: `k8s.io/client-go`, `k8s.io/api` and `k8s.io/apimachinery` are v0.37.2 before and after, the versions the proofs module chose.

**Modules** (`go list -m all | wc -l`): 121 before; 148 after `go get`; 170 after `go mod tidy` with the test and the two programs in place.

**Binary size** (static, `-trimpath -ldflags "-s -w"`):

| Program | Bytes |
| --- | --- |
| `bin/clientonly` | 26472608 |
| `bin/withkubectl` | 29888672 |
| Difference | 3416064 (3.4 MB, 3.26 MiB) |

The difference is over 3 MB, whether a megabyte is 1,000,000 or 1,048,576 bytes.

**The rules.** `TestRolloutRules` passes, 15 of 15. The log of each case, message and error as the test printed them:

| Case | Message | Done | Error |
| --- | --- | --- | --- |
| a | `deployment "orders" successfully rolled out\n` | true | none |
| b | `Waiting for deployment "orders" rollout to finish: 2 out of 3 new replicas have been updated...\n` | false | none |
| c | `Waiting for deployment "orders" rollout to finish: 1 old replicas are pending termination...\n` | false | none |
| d | `Waiting for deployment "orders" rollout to finish: 2 of 3 updated replicas are available...\n` | false | none |
| e | `Waiting for deployment spec update to be observed...\n` | false | none |
| f | empty | false | `deployment "orders" exceeded its progress deadline` |
| g | `deployment "orders" successfully rolled out\n` | true | none |
| h | `partitioned roll out complete: 3 new pods have been updated...\n` | true | none |
| i | `Waiting for partitioned roll out to finish: 2 out of 3 new pods have been updated...\n` | false | none |
| j | `partitioned roll out complete: 1 new pods have been updated...\n` | true | none |
| k | empty | true | `rollout status is only available for RollingUpdate strategy type` |
| l | `daemon set "agent" successfully rolled out\n` | true | none |
| m | `Waiting for daemon set "agent" rollout to finish: 2 of 3 updated pods are available...\n` | false | none |
| n | empty | true | `rollout status is only available for RollingUpdate strategy type` |
| o | `deployment "orders" successfully rolled out\n` | true | none |

What the cases show beyond the assertions:

- Case k and case n: for an `OnDelete` update strategy the rule returns done **true together with an error**. A caller that tests only `done` takes a StatefulSet or DaemonSet on `OnDelete` as finished; the tool must test the error first.
- Case j: with partition 2 and one updated replica of three, the rule returns done true ("partitioned roll out complete"), because one updated replica is 3 minus the partition. A partitioned StatefulSet is finished by this rule while a pod still runs the old revision.
- Case h and case i: with `rollingUpdate` set, which the API server always fills in for `RollingUpdate`, the rule returns at the partition branch. It never compares `currentRevision` with `updateRevision`; that comparison is reached only when `rollingUpdate` is nil. In case i the rule says false because `updatedReplicas` is under the partition-adjusted count, not because the revisions differ.
- Case o: no rule waits for terminating pods. The Deployment with `terminatingReplicas` 2 is done. The tool adds that condition itself.
- Case g: an object whose generation was not changed, with `observedGeneration` equal to it, is done when its counts are right.

## Decision

The tool **copies** the three rules and does not import kubectl's package: importing the package makes the binary 3416064 bytes larger, over the 3 MB the plan set as the limit, and it adds 49 modules (121 to 170) to the proofs module. The copy keeps the licence header and names its source file and tag, and a test in the tool's module compares the copy with kubectl's package on the same objects. The tool's module therefore needs kubectl's package in its test code only, so that the comparing test can run; the binary does not link it.

For Task 25, the conditions of each rule, in the order the rule tests them. `Status(obj, revision)` is called with revision 0 here; with a revision above 0 the Deployment rule first compares the revision and returns an error when it differs, and the other two rules ignore it.

Deployment (`DeploymentStatusViewer`):

1. The object cannot be converted: return no message, done false and an error.
2. Revision above 0 and the Deployment's revision annotation differs or cannot be read: done false and an error. (Not used with revision 0.)
3. `generation > status.observedGeneration`: message `Waiting for deployment spec update to be observed...`, done false. The rest are tested only when `generation <= observedGeneration`:
4. A condition of type `Progressing` exists and its reason is `ProgressDeadlineExceeded`: done false and the error `deployment "NAME" exceeded its progress deadline`.
5. `spec.replicas` is set and `status.updatedReplicas < spec.replicas`: done false ("N out of M new replicas have been updated").
6. `status.replicas > status.updatedReplicas`: done false ("N old replicas are pending termination").
7. `status.availableReplicas < status.updatedReplicas`: done false ("N of M updated replicas are available").
8. Otherwise: `deployment "NAME" successfully rolled out`, done true.

The rule never reads `readyReplicas` or `terminatingReplicas`, and does not test that `availableReplicas` equals `spec.replicas`.

StatefulSet (`StatefulSetStatusViewer`):

1. The object cannot be converted: done false and an error.
2. `spec.updateStrategy.type` is not `RollingUpdate`: no message, done **true** and the error `rollout status is only available for RollingUpdate strategy type`.
3. `status.observedGeneration == 0` or `generation > status.observedGeneration`: `Waiting for statefulset spec update to be observed...`, done false.
4. `spec.replicas` is set and `status.readyReplicas < spec.replicas`: `Waiting for N pods to be ready...`, done false.
5. The strategy is `RollingUpdate` and `spec.updateStrategy.rollingUpdate` is not nil:
   1. `spec.replicas` and `rollingUpdate.partition` are both set and `status.updatedReplicas < spec.replicas - partition`: done false ("Waiting for partitioned roll out to finish").
   2. Otherwise: `partitioned roll out complete: N new pods have been updated...`, done true. The rule returns here.
6. (Reached only when `rollingUpdate` is nil.) `status.updateRevision != status.currentRevision`: done false.
7. Otherwise: `statefulset rolling update complete N pods at revision R...`, done true.

DaemonSet (`DaemonSetStatusViewer`):

1. The object cannot be converted: done false and an error.
2. `spec.updateStrategy.type` is not `RollingUpdate`: no message, done **true** and the error `rollout status is only available for RollingUpdate strategy type`.
3. `generation > status.observedGeneration`: `Waiting for daemon set spec update to be observed...`, done false. The rest are tested only when `generation <= observedGeneration`:
4. `status.updatedNumberScheduled < status.desiredNumberScheduled`: done false ("N out of M new pods have been updated").
5. `status.numberAvailable < status.desiredNumberScheduled`: done false ("N of M updated pods are available").
6. Otherwise: `daemon set "NAME" successfully rolled out`, done true.

The rule never reads `numberReady`.

The licence header of `rollout_status.go`, as it stands:

```
/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
```

Plan assumed: "The tool copies the three Status rules of k8s.io/kubectl/pkg/polymorphichelpers (rollout_status.go at the tag that matches client-go) into internal/watch/rollout.go, with their licence header and source, and a test in the tool's module compares the copy with kubectl's package on the same objects. Importing the package into the binary costs more than 3 MB."
Holds: yes
