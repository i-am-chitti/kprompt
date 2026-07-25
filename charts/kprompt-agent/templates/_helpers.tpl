{{/*
Expand the name of the chart.
*/}}
{{- define "kprompt-agent.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "kprompt-agent.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{- define "kprompt-agent.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "kprompt-agent.labels" -}}
helm.sh/chart: {{ include "kprompt-agent.chart" . }}
{{ include "kprompt-agent.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "kprompt-agent.selectorLabels" -}}
app.kubernetes.io/name: {{ include "kprompt-agent.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "kprompt-agent.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "kprompt-agent.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{- define "kprompt-agent.watchNamespace" -}}
{{- if .Values.watchNamespace }}
{{- .Values.watchNamespace }}
{{- else }}
{{- .Release.Namespace }}
{{- end }}
{{- end }}

{{- define "kprompt-agent.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion }}
{{- printf "%s:%s" .Values.image.repository $tag }}
{{- end }}

{{- define "kprompt-agent.secretName" -}}
{{- .Values.secret.name }}
{{- end }}

{{- define "kprompt-agent.agentCRNamespace" -}}
{{- if .Values.agentCR.namespace }}
{{- .Values.agentCR.namespace }}
{{- else }}
{{- include "kprompt-agent.watchNamespace" . }}
{{- end }}
{{- end }}
