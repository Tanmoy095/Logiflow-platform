{{/*
Standard labels applied to every resource.
Uses .Chart.Name so each service gets its own name.
*/}}
{{- define "logiflow.labels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/part-of: logiflow
{{- end }}

{{/*
Selector labels that MUST match between Deployment and Service.
These are the labels used in selector.matchLabels and the Service’s selector.
*/}}
{{- define "logiflow.selectorLabels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Pod-level security context. Applied to the pod spec.
*/}}
{{- define "logiflow.podSecurityContext" -}}
runAsNonRoot: true
{{- end }}

{{/*
Container-level security context. Every container inherits this.
This is where the read-only root filesystem is enforced.
*/}}
{{- define "logiflow.containerSecurityContext" -}}
readOnlyRootFilesystem: true
runAsUser: 1000
capabilities:
  drop:
    - ALL
{{- end }}

{{/*
Default resource requests and limits.
Services can override by setting .Values.resources in their values.yaml.
*/}}
{{- define "logiflow.defaultResources" -}}
requests:
  cpu: 100m
  memory: 128Mi
limits:
  cpu: 200m
  memory: 256Mi
{{- end }}

{{/*
Default readiness probe. Uses .Values.probes.readiness.path if set,
otherwise /healthz. The port comes from .Values.service.port.
*/}}
{{- define "logiflow.readinessProbe" -}}
httpGet:
  path: {{ .Values.probes.readiness.path | default "/healthz" }}
  port: {{ .Values.service.port }}
initialDelaySeconds: {{ .Values.probes.readiness.initialDelaySeconds | default 3 }}
periodSeconds: {{ .Values.probes.readiness.periodSeconds | default 5 }}
failureThreshold: 2
{{- end }}