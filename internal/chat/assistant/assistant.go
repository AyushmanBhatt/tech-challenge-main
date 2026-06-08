package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/acai-travel/tech-challenge/internal/chat/model"
	ics "github.com/arran4/golang-ical"
	"github.com/openai/openai-go/v2"
)

type Assistant struct {
	cli openai.Client
}

func New() *Assistant {
	return &Assistant{cli: openai.NewClient()}
}

func (a *Assistant) Title(ctx context.Context, conv *model.Conversation) (string, error) {
	if len(conv.Messages) == 0 {
		return "An empty conversation", nil
	}

	slog.InfoContext(ctx, "Generating title for conversation", "conversation_id", conv.ID)

	// For performance and to avoid the model answering the prompt, only send
	// a concise instruction plus the last user message. This encourages a
	// short summarizing title instead of an answer.
	var lastUser string
	for i := len(conv.Messages) - 1; i >= 0; i-- {
		if conv.Messages[i].Role == model.RoleUser {
			lastUser = conv.Messages[i].Content
			break
		}
	}
	if strings.TrimSpace(lastUser) == "" {
		// Fallback to the last message if no user message found (shouldn't happen)
		lastUser = conv.Messages[len(conv.Messages)-1].Content
	}

	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("You are a concise title generator. Create a short title that summarizes the user's message without answering it. The title should be a single line, no more than 80 characters, and should not include special characters, punctuation, or emojis."),
		openai.UserMessage(lastUser),
	}

	resp, err := a.cli.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:    openai.ChatModelO1,
		Messages: msgs,
	})

	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 || strings.TrimSpace(resp.Choices[0].Message.Content) == "" {
		return "", errors.New("empty response from OpenAI for title generation")
	}

	title := resp.Choices[0].Message.Content
	title = strings.ReplaceAll(title, "\n", " ")
	title = strings.Trim(title, " \t\r\n-\"'")

	if len(title) > 80 {
		title = title[:80]
	}

	return title, nil
}

func (a *Assistant) Reply(ctx context.Context, conv *model.Conversation) (string, error) {
	if len(conv.Messages) == 0 {
		return "", errors.New("conversation has no messages")
	}

	slog.InfoContext(ctx, "Generating reply for conversation", "conversation_id", conv.ID)

	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("You are a helpful, concise AI assistant. Provide accurate, safe, and clear responses."),
	}

	var collectedToolOutputs []string

	for _, m := range conv.Messages {
		switch m.Role {
		case model.RoleUser:
			msgs = append(msgs, openai.UserMessage(m.Content))
		case model.RoleAssistant:
			msgs = append(msgs, openai.AssistantMessage(m.Content))
		}
	}

	for i := 0; i < 15; i++ {
		resp, err := a.cli.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
			Model:    openai.ChatModelGPT4_1,
			Messages: msgs,
			Tools: []openai.ChatCompletionToolUnionParam{
				openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
					Name:        "get_weather",
					Description: openai.String("Get weather at the given location"),
					Parameters: openai.FunctionParameters{
						"type": "object",
						"properties": map[string]any{
							"location": map[string]string{
								"type": "string",
							},
						},
						"required": []string{"location"},
					},
				}),
				openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
					Name:        "get_today_date",
					Description: openai.String("Get today's date and time in RFC3339 format"),
				}),
				openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
					Name:        "get_holidays",
					Description: openai.String("Gets local bank and public holidays. Each line is a single holiday in the format 'YYYY-MM-DD: Holiday Name'."),
					Parameters: openai.FunctionParameters{
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
				}),
			},
		})

		if err != nil {
			return "", err
		}

		if len(resp.Choices) == 0 {
			return "", errors.New("no choices returned by OpenAI")
		}

		if message := resp.Choices[0].Message; len(message.ToolCalls) > 0 {
			msgs = append(msgs, message.ToParam())

			for _, call := range message.ToolCalls {
				slog.InfoContext(ctx, "Tool call received", "name", call.Function.Name, "args", call.Function.Arguments)

				switch call.Function.Name {
				case "get_weather":
					// Parse tool call arguments
					var payload struct {
						Location string `json:"location"`
					}

					if err := json.Unmarshal([]byte(call.Function.Arguments), &payload); err != nil {
						msgs = append(msgs, openai.ToolMessage("failed to parse tool call arguments: "+err.Error(), call.ID))
						continue
					}

					if strings.TrimSpace(payload.Location) == "" {
						msgs = append(msgs, openai.ToolMessage("location is required", call.ID))
						continue
					}

					// Geocode the location using Open-Meteo geocoding API
					geoURL := "https://geocoding-api.open-meteo.com/v1/search?name=" + url.QueryEscape(payload.Location) + "&count=1"
					var geores struct {
						Results []struct {
							Name      string  `json:"name"`
							Country   string  `json:"country"`
							Latitude  float64 `json:"latitude"`
							Longitude float64 `json:"longitude"`
						} `json:"results"`
					}

					func() {
						ctx2, cancel := context.WithTimeout(ctx, 10*time.Second)
						defer cancel()

						req, _ := http.NewRequestWithContext(ctx2, http.MethodGet, geoURL, nil)
						resp, err := http.DefaultClient.Do(req)
						if err != nil {
							msgs = append(msgs, openai.ToolMessage("geocoding request failed: "+err.Error(), call.ID))
							return
						}
						defer resp.Body.Close()

						b, _ := io.ReadAll(resp.Body)
						if err := json.Unmarshal(b, &geores); err != nil {
							msgs = append(msgs, openai.ToolMessage("failed to parse geocoding response: "+err.Error(), call.ID))
							return
						}
					}()

					if len(geores.Results) == 0 {
						msgs = append(msgs, openai.ToolMessage("location not found: "+payload.Location, call.ID))
						continue
					}

					loc := geores.Results[0]

					// Fetch current weather from Open-Meteo
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

					func() {
						ctx2, cancel := context.WithTimeout(ctx, 10*time.Second)
						defer cancel()

						req, _ := http.NewRequestWithContext(ctx2, http.MethodGet, weatherURL, nil)
						resp, err := http.DefaultClient.Do(req)
						if err != nil {
							msgs = append(msgs, openai.ToolMessage("weather request failed: "+err.Error(), call.ID))
							return
						}
						defer resp.Body.Close()

						b, _ := io.ReadAll(resp.Body)
						if err := json.Unmarshal(b, &weatherRes); err != nil {
							msgs = append(msgs, openai.ToolMessage("failed to parse weather response: "+err.Error(), call.ID))
							return
						}
					}()

					// Map weather code to description (simple mapping)
					var weatherDesc string
					switch weatherRes.CurrentWeather.Weathercode {
					case 0:
						weatherDesc = "Clear sky"
					case 1, 2, 3:
						weatherDesc = "Mainly clear, partly cloudy, or overcast"
					case 45, 48:
						weatherDesc = "Fog or depositing rime fog"
					case 51, 53, 55:
						weatherDesc = "Drizzle"
					case 61, 63, 65:
						weatherDesc = "Rain"
					case 71, 73, 75:
						weatherDesc = "Snow"
					case 80, 81, 82:
						weatherDesc = "Rain showers"
					default:
						weatherDesc = "Unknown conditions"
					}

					// Format response
					w := weatherRes.CurrentWeather
					// Convert wind direction degrees to compass
					windDirDeg := w.Winddirection
					windCompass := func(d float64) string {
						dirs := []string{"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE", "S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"}
						idx := int((d+11.25)/22.5) % 16
						return dirs[idx]
					}(windDirDeg)

					out := fmt.Sprintf("%s, %s\nTemperature: %.1f °C\nWindspeed: %.1f km/h (%s)\nWind direction: %.0f° (%s)\nCondition: %s\nTime (UTC): %s", loc.Name, loc.Country, w.Temperature, w.Windspeed, "km/h", windDirDeg, windCompass, weatherDesc, w.Time)

					// Append the tool message (the model sees this as tool output).

					msgs = append(msgs, openai.ToolMessage(out, call.ID))

					// Also append an assistant message that echoes the tool output so
					// the final assistant reply will contain it verbatim.
					msgs = append(msgs, openai.AssistantMessage("Tool Output:\n"+out))

					// Add an explicit system instruction so the assistant includes the tool output
					// verbatim in its final reply, prefixed with 'Tool Output:'. This encourages
					// the model to surface the tool results to the user.
					instruction := fmt.Sprintf("When you respond, include the most recent tool output (id: %s) verbatim, prefixed with 'Tool Output:'. After the tool output, provide a concise human-friendly summary or next step.", call.ID)
					msgs = append(msgs, openai.SystemMessage(instruction))
				case "get_today_date":
					msgs = append(msgs, openai.ToolMessage(time.Now().Format(time.RFC3339), call.ID))
				case "get_holidays":
					link := "https://www.officeholidays.com/ics/spain/catalonia"
					if v := os.Getenv("HOLIDAY_CALENDAR_LINK"); v != "" {
						link = v
					}

					events, err := LoadCalendar(ctx, link)
					if err != nil {
						msgs = append(msgs, openai.ToolMessage("failed to load holiday events", call.ID))
						break
					}

					var payload struct {
						BeforeDate time.Time `json:"before_date,omitempty"`
						AfterDate  time.Time `json:"after_date,omitempty"`
						MaxCount   int       `json:"max_count,omitempty"`
					}

					if err := json.Unmarshal([]byte(call.Function.Arguments), &payload); err != nil {
						msgs = append(msgs, openai.ToolMessage("failed to parse tool call arguments: "+err.Error(), call.ID))
						break
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

						holidays = append(holidays, date.Format(time.DateOnly)+": "+event.GetProperty(ics.ComponentPropertySummary).Value)
					}

					msgs = append(msgs, openai.ToolMessage(strings.Join(holidays, "\n"), call.ID))
				default:
					return "", errors.New("unknown tool call: " + call.Function.Name)
				}
			}

			continue
		}

		final := resp.Choices[0].Message.Content
		if len(collectedToolOutputs) > 0 {
			final = "Tool Output:\n" + strings.Join(collectedToolOutputs, "\n\n") + "\n\n" + final
		}

		return final, nil
	}

	return "", errors.New("too many tool calls, unable to generate reply")
}
