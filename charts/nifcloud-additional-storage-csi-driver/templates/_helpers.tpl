{{/*
Expand the name of the chart.
*/}}
{{- define "nifcloud-additional-storage-csi-driver.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "nifcloud-additional-storage-csi-driver.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "nifcloud-additional-storage-csi-driver.labels" -}}
helm.sh/chart: {{ include "nifcloud-additional-storage-csi-driver.chart" . }}
{{ include "nifcloud-additional-storage-csi-driver.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "nifcloud-additional-storage-csi-driver.selectorLabels" -}}
app.kubernetes.io/name: {{ include "nifcloud-additional-storage-csi-driver.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

