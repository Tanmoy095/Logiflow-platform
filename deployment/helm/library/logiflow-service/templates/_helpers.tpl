{{/*
Standard labels for all LogiFlow resources
*/}}
{{- define "logiflow.labels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/part-of: logiflow
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels – must be identical in Deployment and Service
*/}}
{{- define "logiflow.selectorLabels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Resource name – clean single‑line name (uses the Helm release name)
*/}}
{{- define "logiflow.name" -}}
{{ .Release.Name }}
{{- end }}

{{/*
Pod-level security context – applied to the whole pod
*/}}
{{- define "logiflow.podSecurityContext" -}}
runAsNonRoot: true
fsGroup: 1000
{{- end }}

{{/*
Container-level security context – hardens every container
Now includes seccompProfile for runtime defense.
*/}}
{{- define "logiflow.containerSecurityContext" -}}
allowPrivilegeEscalation: false
readOnlyRootFilesystem: true
runAsNonRoot: true
runAsUser: 1000
capabilities:
  drop:
    - ALL
seccompProfile:
  type: RuntimeDefault
{{- end }}

{{/*
Default resource requests and limits – prevents noisy neighbours
Uses values.yaml overrides when present, sensible defaults otherwise.
*/}}
{{- define "logiflow.resources" -}}
requests:
  cpu: {{ .Values.resources.requests.cpu | default "100m" }}
  memory: {{ .Values.resources.requests.memory | default "128Mi" }}
limits:
  cpu: {{ .Values.resources.limits.cpu | default "200m" }}
  memory: {{ .Values.resources.limits.memory | default "256Mi" }}
{{- end }}


{{/*
Startup Probe – protects slow‑starting containers from being killed.
Defaults wait up to 30 checks × 5s = 150 seconds.
/startup is a dedicated endpoint that the application must implement – it should return 200 only when everything (DB, models, caches) is initialised.

30 attempts every 5 seconds = up to 150 seconds of patience.

Services that start in 2 seconds will pass on the first check; AI services get plenty of time.
*/}}
{{- define "logiflow.startupProbe" -}}
startupProbe:
  httpGet:
    path: {{ .Values.probes.startup.path | default "/startup" }}
    port: {{ .Values.service.port | default 8080 }}
  failureThreshold: {{ .Values.probes.startup.failureThreshold | default 30 }}
  periodSeconds: {{ .Values.probes.startup.periodSeconds | default 5 }}
{{- end }}

{{/*
Readiness probe – removes pod from Service if not ready
*/}}
{{- define "logiflow.readinessProbe" -}}
httpGet:
  path: {{ .Values.probes.readiness.path | default "/healthz" }}
  port: {{ .Values.service.port | default 8080 }}
initialDelaySeconds: {{ .Values.probes.readiness.initialDelaySeconds | default 3 }}
periodSeconds: {{ .Values.probes.readiness.periodSeconds | default 5 }}
failureThreshold: {{ .Values.probes.readiness.failureThreshold | default 2 }}
{{- end }}

{{/*
Liveness probe – restarts the container if it gets stuck
*/}}
{{- define "logiflow.livenessProbe" -}}
httpGet:
  path: {{ .Values.probes.liveness.path | default "/live" }}
  port: {{ .Values.service.port | default 8080 }}
initialDelaySeconds: {{ .Values.probes.liveness.initialDelaySeconds | default 5 }}
periodSeconds: {{ .Values.probes.liveness.periodSeconds | default 10 }}
failureThreshold: {{ .Values.probes.liveness.failureThreshold | default 3 }}
{{- end }}