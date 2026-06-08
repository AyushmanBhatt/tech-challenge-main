package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"

	"github.com/openai/openai-go/v2"
)

func sunTimesToolHandler(ctx context.Context, call openai.ChatCompletionMessageToolCallUnion) toolResult {
	var payload struct {
		Location string `json:"location"`
	}

	if err := json.Unmarshal([]byte(call.Function.Arguments), &payload); err != nil {
		return toolResult{Message: openai.ToolMessage("failed to parse tool call arguments: "+err.Error(), call.ID)}
	}

	if payload.Location == "" {
		return toolResult{Message: openai.ToolMessage("location is required", call.ID)}
	}

	geoURL := os.Getenv("GEOCODING_API_BASE_URL")
	if geoURL == "" {
		geoURL = "https://geocoding-api.open-meteo.com/v1/search"
	}

	geoQuery, err := url.Parse(geoURL)
	if err != nil {
		return toolResult{Message: openai.ToolMessage("invalid geocoding URL", call.ID)}
	}

	query := geoQuery.Query()
	query.Set("name", payload.Location)
	query.Set("count", "1")
	geoQuery.RawQuery = query.Encode()

	var geores struct {
		Results []struct {
			Name      string  `json:"name"`
			Country   string  `json:"country"`
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		} `json:"results"`
	}

	if err := fetchJSON(ctx, geoQuery.String(), &geores); err != nil {
		return toolResult{Message: openai.ToolMessage("geocoding request failed: "+err.Error(), call.ID)}
	}

	if len(geores.Results) == 0 {
		return toolResult{Message: openai.ToolMessage("location not found: "+payload.Location, call.ID)}
	}

	loc := geores.Results[0]

	sunURL := os.Getenv("SUN_TIMES_API_BASE_URL")
	if sunURL == "" {
		sunURL = "https://api.sunrise-sunset.org/json"
	}

	sunQuery, err := url.Parse(sunURL)
	if err != nil {
		return toolResult{Message: openai.ToolMessage("invalid sun times URL", call.ID)}
	}

	query = sunQuery.Query()
	query.Set("lat", fmt.Sprintf("%f", loc.Latitude))
	query.Set("lng", fmt.Sprintf("%f", loc.Longitude))
	query.Set("formatted", "0")
	sunQuery.RawQuery = query.Encode()

	var sunRes struct {
		Status  string `json:"status"`
		Results struct {
			Sunrise   string `json:"sunrise"`
			Sunset    string `json:"sunset"`
			DayLength string `json:"day_length"`
		} `json:"results"`
	}

	if err := fetchJSON(ctx, sunQuery.String(), &sunRes); err != nil {
		return toolResult{Message: openai.ToolMessage("sun times request failed: "+err.Error(), call.ID)}
	}

	if sunRes.Status != "OK" {
		return toolResult{Message: openai.ToolMessage("failed to retrieve sun times for location", call.ID)}
	}

	out := fmt.Sprintf(
		"%s, %s\nSunrise (UTC): %s\nSunset (UTC): %s\nDay length: %s",
		loc.Name,
		loc.Country,
		sunRes.Results.Sunrise,
		sunRes.Results.Sunset,
		sunRes.Results.DayLength,
	)

	return toolResult{Message: openai.ToolMessage(out, call.ID), Output: out}
}
