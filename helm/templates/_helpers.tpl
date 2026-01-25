{{/*
Expand the name of the chart.
*/}}
{{- define "gnmic-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "gnmic-operator.fullname" -}}
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

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "gnmic-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "gnmic-operator.labels" -}}
helm.sh/chart: {{ include "gnmic-operator.chart" . }}
{{ include "gnmic-operator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "gnmic-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "gnmic-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
control-plane: controller-manager
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "gnmic-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "gnmic-operator.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Webhook service name
*/}}
{{- define "gnmic-operator.webhookServiceName" -}}
{{- printf "%s-webhook" (include "gnmic-operator.fullname" .) }}
{{- end }}

{{/*
Certificate name
*/}}
{{- define "gnmic-operator.certificateName" -}}
{{- printf "%s-serving-cert" (include "gnmic-operator.fullname" .) }}
{{- end }}

{{/*
Issuer name
*/}}
{{- define "gnmic-operator.issuerName" -}}
{{- if .Values.certManager.issuer.name }}
{{- .Values.certManager.issuer.name }}
{{- else }}
{{- printf "%s-issuer" (include "gnmic-operator.fullname" .) }}
{{- end }}
{{- end }}
