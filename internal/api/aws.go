package api

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// AWS Health Dashboard types

type awsEvent struct {
	Date        string        `json:"date"`
	ARN         string        `json:"arn"`
	Service     string        `json:"service"`
	ServiceName string        `json:"service_name"`
	RegionName  string        `json:"region_name"`
	Status      string        `json:"status"`
	Summary     string        `json:"summary"`
	EventLog    []awsEventLog `json:"event_log"`
}

func (e awsEvent) dateUnix() int64 {
	n, _ := strconv.ParseInt(e.Date, 10, 64)
	return n
}

func (e awsEvent) statusInt() int {
	n, _ := strconv.Atoi(e.Status)
	return n
}

// regionCode extracts the region code (e.g. "me-south-1") from the ARN or service field.
func (e awsEvent) regionCode() string {
	if m := awsRegionRe.FindString(e.ARN); m != "" {
		return m
	}
	if m := awsRegionRe.FindString(e.Service); m != "" {
		return m
	}
	return ""
}

var awsRegionRe = regexp.MustCompile(`[a-z]{2}-[a-z]+-\d+`)

type awsEventLog struct {
	Summary   string `json:"summary"`
	Message   string `json:"message"`
	Status    int    `json:"status"`
	Timestamp int64  `json:"timestamp"`
}

type awsRSSFeed struct {
	XMLName xml.Name      `xml:"rss"`
	Channel awsRSSChannel `xml:"channel"`
}

type awsRSSChannel struct {
	Items []awsRSSItem `xml:"item"`
}

type awsRSSItem struct {
	Title       string `xml:"title"`
	PubDate     string `xml:"pubDate"`
	GUID        string `xml:"guid"`
	Description string `xml:"description"`
}

// regionCode extracts a region code from the RSS GUID.
func (item awsRSSItem) regionCode() string {
	if m := awsRegionRe.FindString(item.GUID); m != "" {
		return m
	}
	return ""
}

// Service category for pseudo-components
type awsServiceCategory struct {
	ID       string
	Name     string
	Keywords []string
}

var awsCategories = []awsServiceCategory{
	{ID: "aws-ec2", Name: "EC2 (Compute)", Keywords: []string{"ec2", "auto scaling"}},
	{ID: "aws-s3", Name: "S3 (Storage)", Keywords: []string{"s3", "simple storage"}},
	{ID: "aws-lambda", Name: "Lambda (Serverless)", Keywords: []string{"lambda", "step functions"}},
	{ID: "aws-rds", Name: "Databases", Keywords: []string{"rds", "aurora", "dynamodb", "elasticache", "redshift"}},
	{ID: "aws-vpc", Name: "Networking", Keywords: []string{"vpc", "cloudfront", "route 53", "elb", "elastic load", "direct connect"}},
	{ID: "aws-iam", Name: "IAM (Identity)", Keywords: []string{"iam", "cognito", "sts"}},
	{ID: "aws-ecs", Name: "Containers", Keywords: []string{"ecs", "eks", "fargate", "ecr"}},
	{ID: "aws-sqs", Name: "Messaging", Keywords: []string{"sqs", "sns", "eventbridge", "kinesis"}},
	{ID: "aws-cw", Name: "Monitoring", Keywords: []string{"cloudwatch", "cloudtrail", "config"}},
	{ID: "aws-other", Name: "Other Services", Keywords: nil},
}

func classifyAWSService(serviceName string) string {
	lower := strings.ToLower(serviceName)
	for _, cat := range awsCategories {
		for _, kw := range cat.Keywords {
			if strings.Contains(lower, kw) {
				return cat.ID
			}
		}
	}
	return "aws-other"
}

type AWSClient struct {
	http *http.Client
}

func NewAWSClient() *AWSClient {
	return &AWSClient{
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

func (a *AWSClient) FetchAll() (*DashboardData, error) {
	cutoff := time.Now().AddDate(0, 0, -56) // 8 weeks

	// Fetch current events
	currentEvents, err := a.fetchCurrentEvents()
	if err != nil {
		return nil, fmt.Errorf("fetching AWS current events: %w", err)
	}

	// Fetch RSS for historical data
	rssItems, err := a.fetchRSS()
	if err != nil {
		return nil, fmt.Errorf("fetching AWS RSS feed: %w", err)
	}

	// Convert current events to incidents
	seen := make(map[string]bool)
	var incidents []Incident

	// Track worst active status per category for component status
	categoryStatus := make(map[string]int)

	for _, ev := range currentEvents {
		inc := convertAWSEvent(ev)
		if inc.CreatedAt.Before(cutoff) {
			continue
		}
		if seen[inc.ID] {
			continue
		}
		seen[inc.ID] = true
		incidents = append(incidents, inc)

		// Track active incident status per category
		if inc.Status != "resolved" && inc.Status != "postmortem" {
			catID := classifyAWSService(ev.ServiceName)
			s := ev.statusInt()
			if s > categoryStatus[catID] {
				categoryStatus[catID] = s
			}
		}
	}

	// Convert RSS items (historical) and merge
	for _, item := range rssItems {
		inc := convertAWSRSSItem(item)
		if inc.CreatedAt.Before(cutoff) {
			continue
		}
		if seen[inc.ID] {
			continue
		}
		seen[inc.ID] = true
		incidents = append(incidents, inc)

	}

	// Build pseudo-components from categories
	components := buildAWSComponents(categoryStatus)

	// Determine overall status
	status := StatusIndicator{Indicator: "none", Description: "All Systems Operational"}
	worstStatus := 0
	for _, s := range categoryStatus {
		if s > worstStatus {
			worstStatus = s
		}
	}
	switch worstStatus {
	case 3:
		status = StatusIndicator{Indicator: "critical", Description: "Major System Outage"}
	case 2:
		status = StatusIndicator{Indicator: "major", Description: "Partial System Outage"}
	case 1:
		status = StatusIndicator{Indicator: "minor", Description: "Minor Service Degradation"}
	}

	return &DashboardData{
		Summary: &SummaryResponse{
			Page: Page{
				Name: "AWS",
				URL:  "https://health.aws.amazon.com/health/status",
			},
			Components: components,
			Status:     status,
		},
		Incidents: &IncidentsResponse{
			Incidents: incidents,
		},
		Regions:   allAWSRegions(),
		FetchedAt: time.Now(),
	}, nil
}

func (a *AWSClient) fetchCurrentEvents() ([]awsEvent, error) {
	resp, err := a.http.Get("https://health.aws.amazon.com/public/currentevents")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("current events returned status %d", resp.StatusCode)
	}

	// AWS returns UTF-16 encoded JSON; convert to UTF-8
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading current events body: %w", err)
	}
	utf8Body := decodeUTF16(body)

	var events []awsEvent
	if err := json.NewDecoder(bytes.NewReader(utf8Body)).Decode(&events); err != nil {
		return nil, fmt.Errorf("decoding current events: %w", err)
	}
	return events, nil
}

// decodeUTF16 converts a UTF-16 (LE or BE) byte slice to UTF-8.
// If the input is already valid UTF-8 JSON, it is returned as-is.
func decodeUTF16(b []byte) []byte {
	if len(b) < 2 {
		return b
	}

	var u16 []uint16
	if b[0] == 0xFF && b[1] == 0xFE {
		// Little-endian BOM
		b = b[2:]
		for i := 0; i+1 < len(b); i += 2 {
			u16 = append(u16, uint16(b[i])|uint16(b[i+1])<<8)
		}
	} else if b[0] == 0xFE && b[1] == 0xFF {
		// Big-endian BOM
		b = b[2:]
		for i := 0; i+1 < len(b); i += 2 {
			u16 = append(u16, uint16(b[i])<<8|uint16(b[i+1]))
		}
	} else {
		// No BOM — check if it looks like UTF-16 LE (null bytes in even positions)
		if len(b) >= 4 && b[1] == 0 {
			for i := 0; i+1 < len(b); i += 2 {
				u16 = append(u16, uint16(b[i])|uint16(b[i+1])<<8)
			}
		} else {
			return b
		}
	}

	runes := utf16.Decode(u16)
	var buf bytes.Buffer
	tmp := make([]byte, utf8.UTFMax)
	for _, r := range runes {
		n := utf8.EncodeRune(tmp, r)
		buf.Write(tmp[:n])
	}
	return buf.Bytes()
}

func (a *AWSClient) fetchRSS() ([]awsRSSItem, error) {
	resp, err := a.http.Get("https://status.aws.amazon.com/rss/all.rss")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("RSS feed returned status %d", resp.StatusCode)
	}

	var feed awsRSSFeed
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("decoding RSS feed: %w", err)
	}
	return feed.Channel.Items, nil
}

func convertAWSEvent(ev awsEvent) Incident {
	createdAt := time.Unix(ev.dateUnix(), 0)

	name := ev.Summary
	if len(name) > 80 {
		name = name[:77] + "..."
	}

	impact := mapAWSStatusToImpact(ev.statusInt())

	// Determine incident status from event log
	incStatus := "investigating"
	var resolvedAt *time.Time
	if len(ev.EventLog) > 0 {
		latest := ev.EventLog[len(ev.EventLog)-1]
		latestLower := strings.ToLower(latest.Summary + " " + latest.Message)
		if strings.Contains(latestLower, "resolved") || strings.Contains(latestLower, "operating normally") {
			incStatus = "resolved"
			t := time.Unix(latest.Timestamp, 0)
			resolvedAt = &t
		}
	}

	// Convert event log to incident updates
	var updates []IncidentUpdate
	for i, entry := range ev.EventLog {
		body := entry.Message
		if body == "" {
			body = entry.Summary
		}
		updates = append(updates, IncidentUpdate{
			ID:        fmt.Sprintf("%s-update-%d", ev.Service, i),
			Status:    mapAWSEventLogStatus(entry),
			Body:      body,
			CreatedAt: time.Unix(entry.Timestamp, 0),
			UpdatedAt: time.Unix(entry.Timestamp, 0),
		})
	}

	id := fmt.Sprintf("aws-%s-%s", ev.Service, ev.Date)

	return Incident{
		ID:              id,
		Name:            name,
		Status:          incStatus,
		CreatedAt:       createdAt,
		UpdatedAt:       createdAt,
		StartedAt:       createdAt,
		ResolvedAt:      resolvedAt,
		Impact:          impact,
		Shortlink:       "https://health.aws.amazon.com/health/status",
		RegionCode:      ev.regionCode(),
		IncidentUpdates: updates,
		Components: []Component{
			{ID: classifyAWSService(ev.ServiceName), Name: ev.ServiceName},
		},
	}
}

func convertAWSRSSItem(item awsRSSItem) Incident {
	createdAt := parseAWSRSSTime(item.PubDate)

	name := item.Title
	if len(name) > 80 {
		name = name[:77] + "..."
	}

	impact := heuristicAWSImpact(item.Title)

	status := "resolved"
	resolvedAt := createdAt

	return Incident{
		ID:         fmt.Sprintf("aws-rss-%s", item.GUID),
		Name:       name,
		Status:     status,
		CreatedAt:  createdAt,
		UpdatedAt:  createdAt,
		StartedAt:  createdAt,
		ResolvedAt: &resolvedAt,
		Impact:     impact,
		Shortlink:  "https://health.aws.amazon.com/health/status",
		RegionCode: item.regionCode(),
		IncidentUpdates: []IncidentUpdate{
			{
				ID:        fmt.Sprintf("aws-rss-%s-0", item.GUID),
				Status:    "resolved",
				Body:      item.Description,
				CreatedAt: createdAt,
				UpdatedAt: createdAt,
			},
		},
	}
}

func buildAWSComponents(categoryStatus map[string]int) []Component {
	var components []Component
	for i, cat := range awsCategories {
		status := "operational"
		if s, ok := categoryStatus[cat.ID]; ok {
			status = mapAWSStatusToComponentStatus(s)
		}
		components = append(components, Component{
			ID:       cat.ID,
			Name:     cat.Name,
			Status:   status,
			Position: i,
			Showcase: true,
		})
	}
	return components
}

// BuildAWSComponentsForRegion rebuilds component status for a specific region
// by scanning incidents tagged with that region code.
func BuildAWSComponentsForRegion(incidents []Incident, regionCode string) []Component {
	categoryStatus := make(map[string]int)
	for _, inc := range incidents {
		if inc.RegionCode != regionCode {
			continue
		}
		if inc.Status == "resolved" || inc.Status == "postmortem" {
			continue
		}
		for _, comp := range inc.Components {
			// Use the component ID (which is the category ID) directly
			catID := comp.ID
			// Derive status int from impact
			s := impactToAWSStatus(inc.Impact)
			if s > categoryStatus[catID] {
				categoryStatus[catID] = s
			}
		}
	}
	return buildAWSComponents(categoryStatus)
}

func impactToAWSStatus(impact string) int {
	switch impact {
	case "critical":
		return 3
	case "major":
		return 2
	default:
		return 1
	}
}

func mapAWSStatusToImpact(status int) string {
	switch status {
	case 3:
		return "critical"
	case 2:
		return "major"
	default:
		return "minor"
	}
}

func mapAWSStatusToComponentStatus(status int) string {
	switch status {
	case 3:
		return "major_outage"
	case 2:
		return "partial_outage"
	default:
		return "degraded_performance"
	}
}

func mapAWSEventLogStatus(entry awsEventLog) string {
	lower := strings.ToLower(entry.Summary + " " + entry.Message)
	if strings.Contains(lower, "resolved") || strings.Contains(lower, "operating normally") {
		return "resolved"
	}
	if strings.Contains(lower, "identified") {
		return "identified"
	}
	if strings.Contains(lower, "monitoring") {
		return "monitoring"
	}
	return "investigating"
}

func heuristicAWSImpact(title string) string {
	lower := strings.ToLower(title)
	if strings.Contains(lower, "outage") || strings.Contains(lower, "unavailable") {
		return "critical"
	}
	if strings.Contains(lower, "degraded") || strings.Contains(lower, "disruption") || strings.Contains(lower, "error") {
		return "major"
	}
	return "minor"
}

func parseAWSRSSTime(s string) time.Time {
	formats := []string{
		time.RFC1123,
		time.RFC1123Z,
		time.RFC822,
		time.RFC822Z,
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// allAWSRegions returns the full list of AWS regions, sorted by code.
func allAWSRegions() []Region {
	names := awsRegionNames()
	regions := make([]Region, 0, len(names))
	for code, name := range names {
		regions = append(regions, Region{Code: code, Name: name})
	}
	sort.Slice(regions, func(i, j int) bool {
		return regions[i].Code < regions[j].Code
	})
	return regions
}

// awsRegionDisplayName returns a human-friendly name for a region code.
func awsRegionDisplayName(code string) string {
	if name, ok := awsRegionNames()[code]; ok {
		return name
	}
	return code
}

func awsRegionNames() map[string]string {
	return map[string]string{
		"us-east-1":      "N. Virginia",
		"us-east-2":      "Ohio",
		"us-west-1":      "N. California",
		"us-west-2":      "Oregon",
		"af-south-1":     "Cape Town",
		"ap-east-1":      "Hong Kong",
		"ap-south-1":     "Mumbai",
		"ap-south-2":     "Hyderabad",
		"ap-southeast-1": "Singapore",
		"ap-southeast-2": "Sydney",
		"ap-southeast-3": "Jakarta",
		"ap-southeast-4": "Melbourne",
		"ap-northeast-1": "Tokyo",
		"ap-northeast-2": "Seoul",
		"ap-northeast-3": "Osaka",
		"ca-central-1":   "Canada",
		"ca-west-1":      "Calgary",
		"eu-central-1":   "Frankfurt",
		"eu-central-2":   "Zurich",
		"eu-west-1":      "Ireland",
		"eu-west-2":      "London",
		"eu-west-3":      "Paris",
		"eu-south-1":     "Milan",
		"eu-south-2":     "Spain",
		"eu-north-1":     "Stockholm",
		"il-central-1":   "Tel Aviv",
		"me-south-1":     "Bahrain",
		"me-central-1":   "UAE",
		"sa-east-1":      "Sao Paulo",
	}
}
