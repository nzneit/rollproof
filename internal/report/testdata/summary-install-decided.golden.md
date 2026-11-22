### rollproof: NOT VERIFIED `orders (Helm) on Kubernetes v1.37.0 in kind`

```
releases    previous   orders 1.4.2, oci://registry.example/charts/orders; chart defaults
            candidate  orders 1.5.0, ./chart; values: inline (image)
images      orders     run by no watched pod
probe       built-in HTTP: GET http://orders.orders.svc/api/orders/latest
            200 a second (one check every 5 ms), timeout 2 s, status 200-299
install     settled after 15.0 s under the probe; NOT VERIFIED
            1,800 checks, 0 failed
            slowest 44 ms, behind 9 ms
```

```
install: no observation for 6.0 s, from 09:59:42.00   [probe-silent]
results: /tmp/rollproof-results-7f3a
```
