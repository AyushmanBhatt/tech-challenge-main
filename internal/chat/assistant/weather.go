package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/openai/openai-go/v2"
)

func newWeatherTool() tool {
	return newTool(
		"get_weather",
		"Get weather at the given location",
		openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"location": map[string]string{
					"type": "string",
				},
			},
			"required": []string{"location"},
		},
		weatherToolHandler,
	)
}

func weatherToolHandler(ctx context.Context, call openai.ChatCompletionMessageToolCallUnion) toolResult {
	var payload struct {
		Location string `json:"location"`
	}

	if err := json.Unmarshal([]byte(call.Function.Arguments), &payload); err != nil {
		return toolResult{Message: openai.ToolMessage("failed to parse tool call arguments: "+err.Error(), call.ID)}
	}

	if payload.Location == "" {
		return toolResult{Message: openai.ToolMessage("location is required", call.ID)}
	}

	geoURL := "https://geocoding-api.open-meteo.com/v1/search?name=" + url.QueryEscape(payload.Location) + "&count=1"
	var geores struct {
		Results []struct {
			Name      string  `json:"name"`
			Country   string  `json:"country"`
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		} `json:"results"`
	}

	if err := fetchJSON(ctx, geoURL, &geores); err != nil {
		return toolResult{Message: openai.ToolMessage("geocoding request failed: "+err.Error(), call.ID)}
	}

	if len(geores.Results) == 0 {
		return toolResult{Message: openai.ToolMessage("location not found: "+payload.Location, call.ID)}
	}

	loc := geores.Results[0]
	weatherURL := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%f&longitude=%f&current_weather=true&timezone=UTC", loc.Latitude, loc.Longitude)
	var weatherRes struct {
		CurrentWeather struct {
			Temperature   float64 `json:"temperature"`
			Windspeed     float64 `json:"windspeed"`
			Winddirection float64 `json:"winddirection"`
			Weathercode   int     `json:"weathercode"`
			Time          string  `json:"time"`
		} `json:"current_weather"`
	}

	if err := fetchJSON(ctx, weatherURL, &weatherRes); err != nil {
		return toolResult{Message: openai.ToolMessage("weather request failed: "+err.Error(), call.ID)}
	}

	w := weatherRes.CurrentWeather
	out := fmt.Sprintf(
		"%s, %s\nTemperature: %.1f °C\nWindspeed: %.1f km/h\nWind direction: %.0f° (%s)\nCondition: %s\nTime (UTC): %s",
		loc.Name,
		loc.Country,
		w.Temperature,
		w.Windspeed,
		w.Winddirection,
		windCompass(w.Winddirection),
		weatherDescription(w.Weathercode),
		w.Time,
	)

	return toolResult{Message: openai.ToolMessage(out, call.ID), Output: out}
}

func fetchJSON(ctx context.Context, url string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	return json.Unmarshal(body, out)
}

func windCompass(direction float64) string {
	dirs := []string{"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE", "S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"}
	idx := int((direction+11.25)/22.5) % len(dirs)
	return dirs[idx]
}

func weatherDescription(code int) string {
	switch code {
	case 0:
		return "Clear sky"
	case 1, 2, 3:
		return "Mainly clear, partly cloudy, or overcast"
	case 45, 48:
		return "Fog or depositing rime fog"
	case 51, 53, 55:
		return "Drizzle"
	case 61, 63, 65:
		return "Rain"
	case 71, 73, 75:
		return "Snow"
	case 80, 81, 82:
		return "Rain showers"
	default:
		return "Unknown conditions"
	}
}
