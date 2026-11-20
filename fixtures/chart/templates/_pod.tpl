{{/*
The pod spec of the orders workload, shared by the Deployment, the StatefulSet and the
DaemonSet. A DaemonSet keeps its anti-affinity off: the extra pod of a rollout shares its
node with the pod it replaces.
*/}}
{{- define "orders.podSpec" -}}
terminationGracePeriodSeconds: {{ .Values.gracePeriod }}
automountServiceAccountToken: false
securityContext:
  runAsNonRoot: true
  runAsUser: 65532
  runAsGroup: 65532
  seccompProfile:
    type: RuntimeDefault
{{- if and .Values.spread (ne .Values.workload "DaemonSet") }}
affinity:
  podAntiAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      - labelSelector:
          matchLabels:
            app.kubernetes.io/name: orders
        topologyKey: kubernetes.io/hostname
{{- end }}
containers:
  - name: orders
    image: "{{ .Values.image.repository }}@{{ .Values.image.digest }}"
    imagePullPolicy: IfNotPresent
    ports:
      - name: http
        containerPort: 8080
    env:
      - name: START_DELAY
        value: {{ .Values.startDelay | quote }}
      - name: SHUTDOWN
        value: {{ .Values.shutdown | quote }}
      - name: SERVE_AFTER_TERM
        value: {{ .Values.serveAfterTerm | quote }}
      - name: DRAIN_DEADLINE
        value: {{ .Values.drainDeadline | quote }}
      - name: FAIL_WORK
        value: {{ .Values.failWork | quote }}
      - name: FAIL_AFTER
        value: {{ .Values.failAfter | quote }}
      - name: FAIL_FOR
        value: {{ .Values.failFor | quote }}
    securityContext:
      allowPrivilegeEscalation: false
      capabilities:
        drop: ["ALL"]
{{- if .Values.readiness }}
    readinessProbe:
      httpGet:
        path: /readyz
        port: 8080
      periodSeconds: 1
      failureThreshold: 1
{{- end }}
{{- end }}
