package main

import (
	"os"
	"testing"
)

func TestScrape(t *testing.T) {

	// TestSuccessfulConnection
	exporter, _ := NewExporter(os.Getenv("GOOGLE_PROJECT_ID"), "")
	projectMap := exporter.scrape()["GOOGLE_PROJECT_ID"]
	projectUp := projectMap.project
	regionsUp := projectMap.regionList
	if projectUp == nil {
		t.Errorf("TestSuccessfulConnection: projectUp=0, expected=1")
	}
	if regionsUp == nil {
		t.Errorf("TestSuccessfulConnection: regionsUp=0, expected=1")
	}

	// TestFailedConnection
	// Set the project name to "503" since the Google Compute API will append this to the end of the BasePath
	exporter, _ = NewExporter("503", "")
	exporter.computeService.BasePath  = "http://httpstat.us/"
	exporter.serviceusageService.BasePath = "http://httpstat.us/"
	exporter.resourceManagerService.BasePath = "http://httpstat.us/"
	projectMap = exporter.scrape()["503"]
	projectUp = projectMap.project
	regionsUp = projectMap.regionList
	if projectUp != nil {
		t.Errorf("TestFailedConnection: projectUp=1, expected=0")
	}
	if regionsUp != nil {
		t.Errorf("TestFailedConnection: regionsUp=1, expected=0")
	}
}
