### rollproof: NOT VERIFIED `orders (Helm) on Kubernetes v1.37.0 in kind`

```
releases    previous   orders 1.4.2, oci://registry.example/charts/orders; chart defaults
            candidate  orders 1.5.0, ./chart; values: inline (image)
images      orders     run by no watched pod
probe       built-in HTTP: GET http://orders.orders.svc/api/orders/latest
            200 a second (one check every 5 ms), timeout 2 s, status 200-299
install     had not settled under the probe when the run ended; NOT VERIFIED
            1,200 checks, 6 failed (refused 5, timeout 1), the last at 09:59:37.10
            slowest 44 ms, behind 9 ms
```

```
install: a signal stopped the run   [interrupted]
results: /tmp/rollproof-results-7f3a
```
