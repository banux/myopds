package main

import "time"

// Metadata metadata struct
type Metadata struct {
	Title      string    `json:"title"`
	Author     string    `json:"author"`
	Identifier string    `json:"identifier"`
	Language   string    `json:"language"`
	Modified   time.Time `json:"modified"`
}

// Link link struct
type Link struct {
	Rel      string `json:"rel,omitempty"`
	Href     string `json:"href"`
	TypeLink string `json:"type"`
	Height   int    `json:"height,omitempty"`
	Width    int    `json:"width,omitempty"`
}

// Manifest manifest struct
type Manifest struct {
	Metadata  Metadata `json:"metadata"`
	Links     []Link   `json:"links"`
	Spine     []Link   `json:"spine,omitempty"`
	Resources []Link   `json:"resources"`
}

// Icon icon struct for AppInstall
type Icon struct {
	Src       string `json:"src"`
	Size      string `json:"size"`
	MediaType string `json:"type"`
}

// AppInstall struct for app install banner
type AppInstall struct {
	ShortName string `json:"short_name"`
	Name      string `json:"name"`
	StartURL  string `json:"start_url"`
	Display   string `json:"display"`
	Icons     Icon   `json:"icons"`
}
