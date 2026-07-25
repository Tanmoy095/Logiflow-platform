{{/*
==========================================
Standard Probe Library – LogiFlow Platform
==========================================
Every service receives the same production‑grade health checks.
Only probe paths may be customised via values.yaml;
all timings and thresholds are fixed by the platform.
*/}}
{{- define "logiflow.probes" -}}

startupProbe:
  httpGet:
    path: {{ .Values.probes.startup.path | default "/startupz" }}
    port: {{ .Values.service.port }}
  periodSeconds: 5
  failureThreshold: 30

readinessProbe:
  httpGet:
    path: {{ .Values.probes.readiness.path | default "/healthz" }}
    port: {{ .Values.service.port }}
  initialDelaySeconds: 3
  periodSeconds: 5
  failureThreshold: 2

livenessProbe:
  httpGet:
    path: {{ .Values.probes.liveness.path | default "/live" }}
    port: {{ .Values.service.port }}
  initialDelaySeconds: 5
  periodSeconds: 10
  failureThreshold: 3

{{- end }}