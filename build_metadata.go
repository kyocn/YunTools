package main

import (
	_ "embed"
	"encoding/json"
)

//go:embed mygo.json
var buildConfig []byte

type buildMetadata struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Updates struct {
		GitHub string `json:"github"`
	} `json:"updates"`
}

func metadata() buildMetadata {
	var m buildMetadata
	if err := json.Unmarshal(buildConfig, &m); err != nil {
		panic(err)
	}
	return m
}
