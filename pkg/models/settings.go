package models

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

const DefaultAPIURL = "https://api.doit.com"

type PluginSettings struct {
	APIURL  string                `json:"apiUrl"`
	Secrets *SecretPluginSettings `json:"-"`
}

type SecretPluginSettings struct {
	APIKey string `json:"apiKey"`
}

func LoadPluginSettings(source backend.DataSourceInstanceSettings) (*PluginSettings, error) {
	settings := PluginSettings{}

	err := json.Unmarshal(source.JSONData, &settings)
	if err != nil {
		return nil, fmt.Errorf("could not unmarshal PluginSettings json: %w", err)
	}

	if settings.APIURL == "" {
		settings.APIURL = DefaultAPIURL
	}

	settings.APIURL = strings.TrimSuffix(settings.APIURL, "/")
	settings.Secrets = loadSecretPluginSettings(source.DecryptedSecureJSONData)

	return &settings, nil
}

func loadSecretPluginSettings(source map[string]string) *SecretPluginSettings {
	return &SecretPluginSettings{
		APIKey: source["apiKey"],
	}
}
