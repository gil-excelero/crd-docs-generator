
{{ define "packages" }}

{{ range .packages }}
    <h1 id="{{- packageAnchorID . -}}">
        {{- packageDisplayName . -}}
    </h1>

    {{ with (index .GoPackages 0 )}}
        {{ with .DocComments }}
        <div>
            {{ safe (renderComments .) }}
        </div>
        {{ end }}
    {{ end }}

    {{ range (visibleTypes (sortedTypes .Types))}}
        {{ template "type" .  }}
    {{ end }}
    <hr/>
{{ end }}

{{ end }}