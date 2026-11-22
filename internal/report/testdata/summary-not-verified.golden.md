### rollproof: NOT VERIFIED `orders (Helm) on Kubernetes v1.37.0 in kind`

```
releases    previous   orders 1.4.2, oci://registry.example/charts/orders; chart defaults
            candidate  orders 1.5.0, ./chart; values: inline (image)
```

```
could not test: nothing to watch   [nothing-watched]
  15 s after the install command returned, namespace orders held no Deployment, StatefulSet
  or DaemonSet that matches the selector app=order
results: /tmp/rollproof-results-7f3a
```
