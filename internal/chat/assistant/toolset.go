package assistant

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	ics "github.com/arran4/golang-ical"
	"github.com/openai/openai-go/v2"
)

func newTools() ([]tool, map[string]tool) {
	tools := []tool{
		newWeatherTool(),
		newSunTimesTool(),
		newTodayDateTool(),
		newHolidaysTool(),
	}
	toolByName := make(map[string]tool, len(tools))
	for _, t := range tools {
		toolByName[t.name] = t
	}
	return tools, toolByName
}

func newTodayDateTool() tool {
	return newTool(
		"get_today_date",
		"Get today's date and time in RFC3339 format",
		openai.FunctionParameters{},
		func(ctx context.Context, call openai.ChatCompletionMessageToolCallUnion) toolResult {
			output := time.Now().Format(time.RFC3339)
			return toolResult{Message: openai.ToolMessage(output, call.ID), Output: output}
		},
	)
}

func newHolidaysTool() tool {
	return newTool(
		"get_holidays",
		"Gets local bank and public holidays. Each line is a single holiday in the format 'YYYY-MM-DD: Holiday Name'.",
		openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"before_date": map[string]string{
					"type":        "string",
					"description": "Optional date in RFC3339 format to get holidays before this date. If not provided, all holidays will be returned.",
				},
				"after_date": map[string]string{
					"type":        "string",
					"description": "Optional date in RFC3339 format to get holidays after this date. If not provided, all holidays will be returned.",
				},
				"max_count": map[string]string{
					"type":        "integer",
					"description": "Optional maximum number of holidays to return. If not provided, all holidays will be returned.",
				},
			},
		},
		func(ctx context.Context, call openai.ChatCompletionMessageToolCallUnion) toolResult {
			link := "https://www.officeholidays.com/ics/spain/catalonia"
			if v := os.Getenv("HOLIDAY_CALENDAR_LINK"); v != "" {
				link = v
			}

			events, err := LoadCalendar(ctx, link)
			if err != nil {
				return toolResult{Message: openai.ToolMessage("failed to load holiday events", call.ID)}
			}

			var payload struct {
				BeforeDate time.Time `json:"before_date,omitempty"`
				AfterDate  time.Time `json:"after_date,omitempty"`
				MaxCount   int       `json:"max_count,omitempty"`
			}

			if err := json.Unmarshal([]byte(call.Function.Arguments), &payload); err != nil {
				return toolResult{Message: openai.ToolMessage("failed to parse tool call arguments: "+err.Error(), call.ID)}
			}

			var holidays []string
			for _, event := range events {
				date, err := event.GetAllDayStartAt()
				if err != nil {
					continue
				}

				if payload.MaxCount > 0 && len(holidays) >= payload.MaxCount {
					break
				}

				if !payload.BeforeDate.IsZero() && date.After(payload.BeforeDate) {
					continue
				}

				if !payload.AfterDate.IsZero() && date.Before(payload.AfterDate) {
					continue
				}

				holiday := date.Format(time.DateOnly) + ": " + event.GetProperty(ics.ComponentPropertySummary).Value
				holidays = append(holidays, holiday)
			}

			result := strings.Join(holidays, "\n")
			return toolResult{Message: openai.ToolMessage(result, call.ID), Output: result}
		},
	)
}

func newSunTimesTool() tool {
	return newTool(
		"get_sun_times",
		"Get sunrise, sunset, and day length for the given location",
		openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"location": map[string]string{
					"type": "string",
				},
			},
			"required": []string{"location"},
		},
		sunTimesToolHandler,
	)
}
