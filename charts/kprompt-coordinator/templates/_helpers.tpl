{{- define "kprompt-coordinator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "kprompt-coordinator.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- include "kprompt-coordinator.name" . -}}
{{- end -}}
{{- end -}}

{{- define "kprompt-coordinator.labels" -}}
app.kubernetes.io/name: {{ include "kprompt-coordinator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: kprompt
{{- end -}}

{{- define "kprompt-coordinator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "kprompt-coordinator.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}
