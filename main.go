package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/zalando/go-keyring"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

const baseURL = "https://api.dataforseo.com/v3"
const specURL = "https://raw.githubusercontent.com/dataforseo/OpenApiDocumentation/master/openapi_specification.yaml"
const keyringService = "dataforseo-cli"

type object = map[string]any

type client struct {
	http     *http.Client
	username string
	password string
}

type gridPoint struct {
	Row       int     `json:"row"`
	Column    int     `json:"column"`
	North     int     `json:"north"`
	East      int     `json:"east"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Rank      int     `json:"rank,omitempty"`
	Business  string  `json:"business,omitempty"`
	PlaceID   string  `json:"place_id,omitempty"`
}

func newClient() (*client, error) {
	username := os.Getenv("DATAFORSEO_USERNAME")
	password := os.Getenv("DATAFORSEO_PASSWORD")
	if username == "" && password == "" {
		var err error
		username, err = keyring.Get(keyringService, "username")
		if err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return nil, fmt.Errorf("read credentials: %w", err)
		}
		password, err = keyring.Get(keyringService, "password")
		if err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return nil, fmt.Errorf("read credentials: %w", err)
		}
	}
	if username == "" || password == "" {
		return nil, errors.New("not authenticated; run dfs auth or set DATAFORSEO_USERNAME and DATAFORSEO_PASSWORD")
	}
	return &client{http: &http.Client{Timeout: 90 * time.Second}, username: username, password: password}, nil
}

func normalizePath(path string) string {
	path = strings.TrimPrefix(path, "https://api.dataforseo.com")
	path = strings.TrimPrefix(path, "/v3")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func (c *client) request(ctx context.Context, method string, path string, body any) (object, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, baseURL+normalizePath(path), reader)
	if err != nil {
		return nil, err
	}
	credential := base64.StdEncoding.EncodeToString([]byte(c.username + ":" + c.password))
	req.Header.Set("Authorization", "Basic "+credential)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	var data object
	if err := json.NewDecoder(response.Body).Decode(&data); err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", response.StatusCode, jsonString(data))
	}
	if code := intAt(data, "status_code"); code != 0 && code != 20000 {
		return nil, fmt.Errorf("DataForSEO %d: %s", code, stringAt(data, "status_message"))
	}
	return data, nil
}

func (c *client) postTask(ctx context.Context, path string, payload object) ([]any, error) {
	data, err := c.request(ctx, http.MethodPost, path, []object{payload})
	if err != nil {
		return nil, err
	}
	tasks := arrayAt(data, "tasks")
	if len(tasks) == 0 {
		return nil, errors.New("DataForSEO returned no task")
	}
	task := asObject(tasks[0])
	if code := intAt(task, "status_code"); code != 20000 {
		return nil, fmt.Errorf("DataForSEO %d: %s", code, stringAt(task, "status_message"))
	}
	return arrayAt(task, "result"), nil
}

func asObject(value any) object {
	if result, ok := value.(map[string]any); ok {
		return result
	}
	return object{}
}

func arrayAt(value any, key string) []any {
	items, ok := asObject(value)[key].([]any)
	if !ok {
		return []any{}
	}
	return items
}

func objectAt(value any, key string) object {
	return asObject(asObject(value)[key])
}

func stringAt(value any, key string) string {
	result, _ := asObject(value)[key].(string)
	return result
}

func numberAt(value any, key string) float64 {
	result, _ := asObject(value)[key].(float64)
	return result
}

func intAt(value any, key string) int {
	return int(numberAt(value, key))
}

func taskItems(result []any) []any {
	if len(result) == 0 {
		return []any{}
	}
	return arrayAt(result[0], "items")
}

func localMatches(items []any, target string) []any {
	needle := strings.ToLower(target)
	matches := make([]any, 0)
	for _, item := range items {
		for _, field := range []string{"title", "domain", "url", "phone", "place_id", "cid"} {
			if strings.Contains(strings.ToLower(stringAt(item, field)), needle) {
				matches = append(matches, item)
				break
			}
		}
	}
	return matches
}

func coordinatePin(value string) (string, float64, float64, error) {
	parts := strings.Split(value, ",")
	if len(parts) < 2 || len(parts) > 3 {
		return "", 0, 0, errors.New("pin must use latitude,longitude[,zoom]")
	}
	latitude, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil || latitude < -90 || latitude > 90 {
		return "", 0, 0, errors.New("pin latitude must be between -90 and 90")
	}
	longitude, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil || longitude < -180 || longitude > 180 {
		return "", 0, 0, errors.New("pin longitude must be between -180 and 180")
	}
	zoom := 17
	if len(parts) == 3 && strings.TrimSpace(parts[2]) != "" {
		zoom, err = strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(parts[2]), "z"))
		if err != nil || zoom < 3 || zoom > 21 {
			return "", 0, 0, errors.New("pin zoom must be an integer between 3 and 21")
		}
	}
	return fmt.Sprintf("%.7f,%.7f,%dz", latitude, longitude, zoom), latitude, longitude, nil
}

func locale(location, language string) object {
	return object{"location_name": location, "language_code": language}
}

func merge(values ...object) object {
	result := object{}
	for _, value := range values {
		for key, item := range value {
			result[key] = item
		}
	}
	return result
}

func jsonString(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func printRows(headers []string, rows [][]string) {
	if len(rows) == 0 {
		fmt.Println("no rows")
		return
	}
	writer := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, strings.Join(headers, "\t"))
	for _, row := range rows {
		fmt.Fprintln(writer, strings.Join(row, "\t"))
	}
	writer.Flush()
}

func value(value float64) string {
	if value == 0 {
		return ""
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func addLocaleFlags(command *cobra.Command, location, language *string) {
	command.Flags().StringVarP(location, "location", "l", "United Kingdom", "location name")
	command.Flags().StringVarP(language, "language", "g", "en", "language code")
}

func newRoot() *cobra.Command {
	root := &cobra.Command{Use: "dfs", Short: "DataForSEO CLI", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newAuth(), newSerp(), newVolume(), newSuggestions(), newRanked(), newCompetitors(), newDifficulty(), newLocalRank(), newLocalGrid(), newBalance(), newAPI())
	return root
}

func newAuth() *cobra.Command {
	command := &cobra.Command{Use: "auth", Short: "authenticate with DataForSEO", Args: cobra.NoArgs, RunE: func(command *cobra.Command, _ []string) error {
		reader := bufio.NewReader(command.InOrStdin())
		fmt.Fprint(command.OutOrStdout(), "DataForSEO API login: ")
		username, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		username = strings.TrimSpace(username)
		if username == "" {
			return errors.New("API login is required")
		}
		fmt.Fprint(command.OutOrStdout(), "DataForSEO API password: ")
		passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(command.OutOrStdout())
		if err != nil {
			return fmt.Errorf("read password: %w", err)
		}
		password := strings.TrimSpace(string(passwordBytes))
		if password == "" {
			return errors.New("API password is required")
		}
		candidate := &client{http: &http.Client{Timeout: 30 * time.Second}, username: username, password: password}
		if _, err := candidate.request(command.Context(), http.MethodGet, "/appendix/user_data", nil); err != nil {
			return fmt.Errorf("credentials rejected: %w", err)
		}
		if err := keyring.Set(keyringService, "username", username); err != nil {
			return fmt.Errorf("store API login: %w", err)
		}
		if err := keyring.Set(keyringService, "password", password); err != nil {
			return fmt.Errorf("store API password: %w", err)
		}
		fmt.Fprintln(command.OutOrStdout(), "Authenticated. Credentials saved in the operating system credential vault.")
		return nil
	}}
	command.AddCommand(&cobra.Command{Use: "logout", Short: "remove saved DataForSEO credentials", Args: cobra.NoArgs, RunE: func(command *cobra.Command, _ []string) error {
		for _, account := range []string{"username", "password"} {
			if err := keyring.Delete(keyringService, account); err != nil && !errors.Is(err, keyring.ErrNotFound) {
				return err
			}
		}
		fmt.Fprintln(command.OutOrStdout(), "Saved DataForSEO credentials removed.")
		return nil
	}})
	return command
}

func newSerp() *cobra.Command {
	var location, language string
	var limit int
	var raw bool
	command := &cobra.Command{Use: "serp <keyword>", Short: "Google organic SERP, live", Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		depth := 10
		if limit > 10 {
			depth = 100
		}
		result, err := c.postTask(command.Context(), "/serp/google/organic/live/advanced", merge(locale(location, language), object{"keyword": args[0], "depth": depth}))
		if err != nil {
			return err
		}
		items := make([]any, 0)
		for _, item := range taskItems(result) {
			if stringAt(item, "type") == "organic" {
				items = append(items, item)
			}
		}
		if len(items) > limit {
			items = items[:limit]
		}
		if raw {
			return printJSON(items)
		}
		rows := make([][]string, 0, len(items))
		for _, item := range items {
			rows = append(rows, []string{strconv.Itoa(intAt(item, "rank_absolute")), stringAt(item, "domain"), stringAt(item, "title"), stringAt(item, "url")})
		}
		printRows([]string{"POS", "DOMAIN", "TITLE", "URL"}, rows)
		return nil
	}}
	addLocaleFlags(command, &location, &language)
	command.Flags().IntVarP(&limit, "limit", "n", 10, "results")
	command.Flags().BoolVar(&raw, "json", false, "raw JSON")
	return command
}

func newVolume() *cobra.Command {
	var location, language string
	var raw bool
	command := &cobra.Command{Use: "volume <keywords...>", Short: "Google Ads search volume", Args: cobra.MinimumNArgs(1), RunE: func(command *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.postTask(command.Context(), "/keywords_data/google_ads/search_volume/live", merge(locale(location, language), object{"keywords": args}))
		if err != nil {
			return err
		}
		if raw {
			return printJSON(result)
		}
		rows := make([][]string, 0, len(result))
		for _, item := range result {
			rows = append(rows, []string{stringAt(item, "keyword"), value(numberAt(item, "search_volume")), value(numberAt(item, "cpc")), stringAt(item, "competition")})
		}
		printRows([]string{"KEYWORD", "VOLUME", "CPC", "COMPETITION"}, rows)
		return nil
	}}
	addLocaleFlags(command, &location, &language)
	command.Flags().BoolVar(&raw, "json", false, "raw JSON")
	return command
}

func newSuggestions() *cobra.Command {
	var location, language string
	var limit int
	var raw bool
	command := &cobra.Command{Use: "suggestions <keyword>", Short: "keyword suggestions (Labs)", Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.postTask(command.Context(), "/dataforseo_labs/google/keyword_suggestions/live", merge(locale(location, language), object{"keyword": args[0], "limit": limit, "include_seed_keyword": true}))
		if err != nil {
			return err
		}
		items := taskItems(result)
		if raw {
			return printJSON(items)
		}
		rows := make([][]string, 0, len(items))
		for _, item := range items {
			rows = append(rows, []string{stringAt(item, "keyword"), value(numberAt(objectAt(item, "keyword_info"), "search_volume")), value(numberAt(objectAt(item, "keyword_info"), "cpc")), value(numberAt(objectAt(item, "keyword_properties"), "keyword_difficulty"))})
		}
		printRows([]string{"KEYWORD", "VOLUME", "CPC", "DIFFICULTY"}, rows)
		return nil
	}}
	addLocaleFlags(command, &location, &language)
	command.Flags().IntVarP(&limit, "limit", "n", 25, "results")
	command.Flags().BoolVar(&raw, "json", false, "raw JSON")
	return command
}

func newRanked() *cobra.Command {
	var location, language string
	var limit int
	var raw bool
	command := &cobra.Command{Use: "ranked <domain>", Short: "keywords a domain ranks for (Labs)", Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.postTask(command.Context(), "/dataforseo_labs/google/ranked_keywords/live", merge(locale(location, language), object{"target": args[0], "limit": limit, "order_by": []string{"keyword_data.keyword_info.search_volume,desc"}}))
		if err != nil {
			return err
		}
		items := taskItems(result)
		if raw {
			return printJSON(items)
		}
		rows := make([][]string, 0, len(items))
		for _, item := range items {
			keyword := objectAt(item, "keyword_data")
			serp := objectAt(objectAt(item, "ranked_serp_element"), "serp_item")
			rows = append(rows, []string{stringAt(keyword, "keyword"), strconv.Itoa(intAt(serp, "rank_absolute")), value(numberAt(objectAt(keyword, "keyword_info"), "search_volume")), stringAt(serp, "relative_url")})
		}
		printRows([]string{"KEYWORD", "POS", "VOLUME", "URL"}, rows)
		return nil
	}}
	addLocaleFlags(command, &location, &language)
	command.Flags().IntVarP(&limit, "limit", "n", 25, "results")
	command.Flags().BoolVar(&raw, "json", false, "raw JSON")
	return command
}

func newCompetitors() *cobra.Command {
	var location, language string
	var limit int
	var raw bool
	command := &cobra.Command{Use: "competitors <domain>", Short: "competitor domains by keyword overlap (Labs)", Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.postTask(command.Context(), "/dataforseo_labs/google/competitors_domain/live", merge(locale(location, language), object{"target": args[0], "limit": limit}))
		if err != nil {
			return err
		}
		items := taskItems(result)
		if raw {
			return printJSON(items)
		}
		rows := make([][]string, 0, len(items))
		for _, item := range items {
			organic := objectAt(objectAt(item, "full_domain_metrics"), "organic")
			rows = append(rows, []string{stringAt(item, "domain"), value(numberAt(item, "intersections")), value(numberAt(organic, "count")), value(numberAt(organic, "etv"))})
		}
		printRows([]string{"DOMAIN", "OVERLAP", "KEYWORDS", "ETV"}, rows)
		return nil
	}}
	addLocaleFlags(command, &location, &language)
	command.Flags().IntVarP(&limit, "limit", "n", 15, "results")
	command.Flags().BoolVar(&raw, "json", false, "raw JSON")
	return command
}

func newDifficulty() *cobra.Command {
	var location, language string
	var raw bool
	command := &cobra.Command{Use: "difficulty <keywords...>", Short: "bulk keyword difficulty (Labs)", Args: cobra.MinimumNArgs(1), RunE: func(command *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.postTask(command.Context(), "/dataforseo_labs/google/bulk_keyword_difficulty/live", merge(locale(location, language), object{"keywords": args}))
		if err != nil {
			return err
		}
		items := taskItems(result)
		if raw {
			return printJSON(items)
		}
		rows := make([][]string, 0, len(items))
		for _, item := range items {
			rows = append(rows, []string{stringAt(item, "keyword"), value(numberAt(item, "keyword_difficulty"))})
		}
		printRows([]string{"KEYWORD", "DIFFICULTY"}, rows)
		return nil
	}}
	addLocaleFlags(command, &location, &language)
	command.Flags().BoolVar(&raw, "json", false, "raw JSON")
	return command
}

func newLocalRank() *cobra.Command {
	var pin, location, language string
	var depth int
	var raw bool
	command := &cobra.Command{Use: "local-rank <keyword> <target>", Short: "find a business or domain in a local Google Maps SERP", Args: cobra.ExactArgs(2), RunE: func(command *cobra.Command, args []string) error {
		payload := object{"keyword": args[0], "language_code": language, "depth": depth}
		if pin != "" {
			normalized, _, _, err := coordinatePin(pin)
			if err != nil {
				return err
			}
			payload["location_coordinate"] = normalized
		} else {
			parts := strings.Split(location, ",")
			for index := range parts {
				parts[index] = strings.TrimSpace(parts[index])
			}
			payload["location_name"] = strings.Join(parts, ",")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.postTask(command.Context(), "/serp/google/maps/live/advanced", payload)
		if err != nil {
			return err
		}
		matches := localMatches(taskItems(result), args[1])
		if raw {
			return printJSON(matches)
		}
		if len(matches) == 0 {
			fmt.Printf("not found in top %d\n", depth)
			return nil
		}
		rows := make([][]string, 0, len(matches))
		for _, item := range matches {
			rows = append(rows, []string{strconv.Itoa(intAt(item, "rank_absolute")), strconv.Itoa(intAt(item, "rank_group")), stringAt(item, "title"), stringAt(item, "domain"), stringAt(item, "address"), stringAt(item, "place_id")})
		}
		printRows([]string{"RANK", "MAP_RANK", "BUSINESS", "DOMAIN", "ADDRESS", "PLACE_ID"}, rows)
		return nil
	}}
	command.Flags().StringVar(&pin, "pin", "", "geographic search pin, latitude,longitude[,zoom]")
	command.Flags().StringVarP(&location, "location", "l", "London,England,United Kingdom", "official full DataForSEO location name")
	command.Flags().StringVarP(&language, "language", "g", "en", "language code")
	command.Flags().IntVarP(&depth, "depth", "n", 100, "result depth")
	command.Flags().BoolVar(&raw, "json", false, "raw matching results")
	return command
}

func newLocalGrid() *cobra.Command {
	var pin, language string
	var size, concurrency, depth int
	var spacing float64
	var raw bool
	command := &cobra.Command{Use: "local-grid <keyword> <target>", Short: "run a concurrent Google Maps local ranking grid", Args: cobra.ExactArgs(2), RunE: func(command *cobra.Command, args []string) error {
		_, centreLat, centreLon, err := coordinatePin(pin)
		if err != nil {
			return err
		}
		if size < 1 || size > 15 || size%2 == 0 {
			return errors.New("grid size must be an odd integer from 1 to 15")
		}
		if spacing <= 0 || spacing > 25 {
			return errors.New("spacing must be between 0 and 25 km")
		}
		if concurrency < 1 || concurrency > 100 {
			return errors.New("concurrency must be from 1 to 100")
		}
		radius := size / 2
		latitudeStep := spacing / 111.32
		longitudeStep := spacing / (111.32 * math.Cos(centreLat*math.Pi/180))
		points := make([]gridPoint, size*size)
		for index := range points {
			row := index / size
			column := index % size
			north := radius - row
			east := column - radius
			points[index] = gridPoint{Row: row, Column: column, North: north, East: east, Latitude: centreLat + float64(north)*latitudeStep, Longitude: centreLon + float64(east)*longitudeStep}
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		started := time.Now()
		semaphore := make(chan struct{}, concurrency)
		errorChannel := make(chan error, len(points))
		var wait sync.WaitGroup
		for index := range points {
			wait.Add(1)
			go func(index int) {
				defer wait.Done()
				semaphore <- struct{}{}
				defer func() { <-semaphore }()
				point := &points[index]
				location := fmt.Sprintf("%.7f,%.7f,14z", point.Latitude, point.Longitude)
				result, requestErr := c.postTask(command.Context(), "/serp/google/maps/live/advanced", object{"keyword": args[0], "language_code": language, "depth": depth, "location_coordinate": location})
				if requestErr != nil {
					errorChannel <- requestErr
					return
				}
				matches := localMatches(taskItems(result), args[1])
				if len(matches) > 0 {
					point.Rank = intAt(matches[0], "rank_absolute")
					point.Business = stringAt(matches[0], "title")
					point.PlaceID = stringAt(matches[0], "place_id")
				}
			}(index)
		}
		wait.Wait()
		close(errorChannel)
		if requestErr := <-errorChannel; requestErr != nil {
			return requestErr
		}
		elapsed := time.Since(started)
		if raw {
			return printJSON(object{"keyword": args[0], "target": args[1], "centre": object{"latitude": centreLat, "longitude": centreLon}, "size": size, "spacing_km": spacing, "elapsed_seconds": elapsed.Seconds(), "results": points})
		}
		fmt.Printf("\n%s → %s\n", args[0], args[1])
		ranked := 0
		total := 0
		for row := 0; row < size; row++ {
			values := make([]string, size)
			for column := 0; column < size; column++ {
				rank := points[row*size+column].Rank
				if rank == 0 {
					values[column] = fmt.Sprintf(">%d", depth)
				} else {
					values[column] = strconv.Itoa(rank)
					ranked++
					total += rank
				}
			}
			for _, item := range values {
				fmt.Printf("%5s ", item)
			}
			fmt.Println()
		}
		average := "n/a"
		if ranked > 0 {
			average = fmt.Sprintf("%.2f", float64(total)/float64(ranked))
		}
		fmt.Printf("%d/%d points ranked; average %s; %.1fs\n", ranked, len(points), average, elapsed.Seconds())
		return nil
	}}
	command.Flags().StringVar(&pin, "pin", "", "grid centre coordinate")
	command.MarkFlagRequired("pin")
	command.Flags().IntVar(&size, "size", 5, "odd grid width")
	command.Flags().Float64Var(&spacing, "spacing", 1, "distance between points in kilometres")
	command.Flags().IntVar(&concurrency, "concurrency", 25, "simultaneous live requests")
	command.Flags().StringVarP(&language, "language", "g", "en", "language code")
	command.Flags().IntVarP(&depth, "depth", "n", 100, "result depth")
	command.Flags().BoolVar(&raw, "json", false, "raw grid data")
	return command
}

func newBalance() *cobra.Command {
	return &cobra.Command{Use: "balance", Short: "account balance", Args: cobra.NoArgs, RunE: func(command *cobra.Command, _ []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		data, err := c.request(command.Context(), http.MethodGet, "/appendix/user_data", nil)
		if err != nil {
			return err
		}
		tasks := arrayAt(data, "tasks")
		if len(tasks) == 0 {
			return errors.New("DataForSEO returned no task")
		}
		results := arrayAt(tasks[0], "result")
		if len(results) == 0 {
			return errors.New("DataForSEO returned no user data")
		}
		fmt.Printf("balance: $%s\n", value(numberAt(objectAt(results[0], "money"), "balance")))
		return nil
	}}
}

func loadOpenAPI(ctx context.Context, source string) (object, error) {
	var raw []byte
	var err error
	if source != "" {
		raw, err = os.ReadFile(source)
	} else {
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, specURL, nil)
		if requestErr != nil {
			return nil, requestErr
		}
		response, requestErr := http.DefaultClient.Do(request)
		if requestErr != nil {
			return nil, requestErr
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("could not load OpenAPI spec: HTTP %d", response.StatusCode)
		}
		raw, err = io.ReadAll(response.Body)
	}
	if err != nil {
		return nil, err
	}
	var spec object
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		return nil, err
	}
	if len(objectAt(spec, "paths")) == 0 {
		return nil, errors.New("invalid DataForSEO OpenAPI specification")
	}
	return spec, nil
}

func newAPI() *cobra.Command {
	api := &cobra.Command{Use: "api", Short: "discover and call the complete DataForSEO v3 API"}
	var listSpec string
	list := &cobra.Command{Use: "list [query]", Short: "list official OpenAPI operations", Args: cobra.MaximumNArgs(1), RunE: func(command *cobra.Command, args []string) error {
		spec, err := loadOpenAPI(command.Context(), listSpec)
		if err != nil {
			return err
		}
		needles := []string{}
		if len(args) == 1 {
			needles = strings.Fields(strings.ToLower(args[0]))
		}
		rows := [][]string{}
		for path, methodsValue := range objectAt(spec, "paths") {
			for method, operationValue := range asObject(methodsValue) {
				if method != "get" && method != "post" && method != "put" && method != "patch" && method != "delete" {
					continue
				}
				operation := asObject(operationValue)
				searchable := strings.ToLower(strings.Join([]string{path, method, stringAt(operation, "operationId"), stringAt(operation, "summary"), fmt.Sprint(operation["tags"])}, " "))
				matched := true
				for _, needle := range needles {
					if !strings.Contains(searchable, needle) {
						matched = false
					}
				}
				if matched {
					rows = append(rows, []string{strings.ToUpper(method), path, stringAt(operation, "operationId"), stringAt(operation, "summary")})
				}
			}
		}
		printRows([]string{"METHOD", "PATH", "OPERATION", "SUMMARY"}, rows)
		fmt.Fprintf(os.Stderr, "%d operation(s)\n", len(rows))
		return nil
	}}
	list.Flags().StringVar(&listSpec, "spec", "", "use a local OpenAPI YAML file")
	var describeMethod, describeSpec string
	describe := &cobra.Command{Use: "describe <path>", Short: "show the official OpenAPI definition for an endpoint", Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
		spec, err := loadOpenAPI(command.Context(), describeSpec)
		if err != nil {
			return err
		}
		path := args[0]
		if !strings.HasPrefix(path, "/v3/") {
			path = "/v3" + normalizePath(path)
		}
		definition := asObject(objectAt(spec, "paths")[path])
		if len(definition) == 0 {
			return fmt.Errorf("endpoint not found: %s", args[0])
		}
		var selected any = definition
		if describeMethod != "" {
			selected = definition[strings.ToLower(describeMethod)]
			if selected == nil {
				return fmt.Errorf("method %s not found for %s", describeMethod, args[0])
			}
		}
		return printJSON(selected)
	}}
	describe.Flags().StringVar(&describeMethod, "method", "", "HTTP method")
	describe.Flags().StringVar(&describeSpec, "spec", "", "use a local OpenAPI YAML file")
	var data string
	var rawBody bool
	request := &cobra.Command{Use: "request <method> <path>", Short: "call any DataForSEO endpoint", Args: cobra.ExactArgs(2), RunE: func(command *cobra.Command, args []string) error {
		var body any
		if data != "" {
			if err := json.Unmarshal([]byte(data), &body); err != nil {
				return fmt.Errorf("invalid JSON: %w", err)
			}
			if _, ok := body.(map[string]any); ok && !rawBody {
				body = []any{body}
			}
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := c.request(command.Context(), strings.ToUpper(args[0]), args[1], body)
		if err != nil {
			return err
		}
		return printJSON(result)
	}}
	request.Flags().StringVarP(&data, "data", "d", "", "JSON body; objects are wrapped as a one-task array")
	request.Flags().BoolVar(&rawBody, "raw-body", false, "do not wrap an object body in an array")
	api.AddCommand(list, describe, request)
	return api
}

func main() {
	if err := newRoot().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
