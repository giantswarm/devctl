{{/*
Expand the name of the chart.
*/}}
{{- define "devctl.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "devctl.fullname" -}}
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
Chart label value: at most 63 characters, beginning and ending alphanumeric
(a long version's cut can land on any run of ".", "_" and "-").
*/}}
{{- define "devctl.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimAll "-._" }}
{{- end }}

{{/*
Common labels. The team label is what the Giant Swarm chart validator reads.
*/}}
{{- define "devctl.labels" -}}
helm.sh/chart: {{ include "devctl.chart" . }}
{{ include "devctl.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
application.giantswarm.io/team: {{ index .Chart.Annotations "io.giantswarm.application.team" | quote }}
{{- end }}

{{/*
Selector labels.
*/}}
{{- define "devctl.selectorLabels" -}}
app.kubernetes.io/name: {{ include "devctl.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
ServiceAccount name.
*/}}
{{- define "devctl.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "devctl.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
The installations rollout_wait reads, as devctl serve's --installations file
names them: each with its API, its Dex and the files of the exchange client
mounted from its credentials Secret.
*/}}
{{- define "devctl.installations" -}}
{{- $list := list }}
{{- range $name, $i := .Values.installations }}
{{- $i = $i | default dict }}
{{- $dex := $i.dex | default dict }}
{{- if and (not $i.baseDomain) (or (not $i.apiServer) (not $dex.tokenEndpoint)) }}
{{- fail (printf "installations.%s: set baseDomain, or apiServer and dex.tokenEndpoint" $name) }}
{{- end }}
{{- $dir := printf "/etc/devctl/installations/%s" $name }}
{{- $entry := dict
  "name" $name
  "apiServer" ($i.apiServer | default (printf "https://happaapi.%s" $i.baseDomain))
  "dex" (dict
    "tokenEndpoint" ($dex.tokenEndpoint | default (printf "https://dex.%s/token" $i.baseDomain))
    "issuer" ($dex.issuer | default (ternary (printf "https://dex.%s" $i.baseDomain) "" (not (empty $i.baseDomain))))
    "connectorId" ($dex.connectorId | default "giantswarm-simple-oidc")
    "audience" ($dex.audience | default "dex-k8s-authenticator")
    "clientIdFile" (printf "%s/client-id" $dir)
    "clientSecretFile" (printf "%s/client-secret" $dir)) }}
{{- $list = append $list $entry }}
{{- end }}
{{- dict "installations" $list | toJson }}
{{- end }}

{{/*
The credentials Secret of installation name, i its values.
*/}}
{{- define "devctl.credentialsSecret" -}}
{{- .i.credentialsSecret | default (printf "%s-token-exchange-credentials" .name) }}
{{- end }}
