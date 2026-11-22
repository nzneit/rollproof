### rollproof: FAIL `orders (Helm) on Kubernetes v1.37.0 in kind`

```
releases    previous   orders 1.4.2, oci://registry.example/charts/orders; chart defaults
            candidate  orders 1.5.0, ./chart; values: inline (image)
images      orders     run by deployment/orders, in pods the deploy created
probe       built-in HTTP: GET http://orders.orders.svc/api/orders/latest
            200 a second (one check every 5 ms), timeout 2 s, status 200-299
install     settled after 15.0 s under the probe; failures not judged
            3,000 checks, 6 failed (refused 5, timeout 1), the last 14.9 s before the steady window
            slowest 44 ms, behind 9 ms
```

| window | length | checks | failed | after finish | slowest | behind | result |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| steady | 10.0 s | 2,000 | 0 | - | 39 ms | 5 ms | pass |
| deploy | 29.1 s | 5,820 | 243 | 31 | 198 ms | 12 ms | FAIL |
| rollback | - | - | - | - | - | - | not run |

| transition | workload | probed | pods replaced | quickest stop |
| --- | --- | --- | --- | --- |
| deploy | `deployment/orders` | yes | 2 of 2 | 0.8 s |

```
deploy: 243 checks failed (refused 198, timeout 45), first at 10:00:07.31   [probe-failures]
  stopping then   orders-6d5f9c-x2k4q (previous release), told to stop 0.3 s before
  starting then   none
results: /tmp/rollproof-results-7f3a
```
