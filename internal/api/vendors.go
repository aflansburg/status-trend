package api

// Fetcher abstracts how a vendor's data is retrieved and normalized.
type Fetcher interface {
	FetchAll() (*DashboardData, error)
}

type Vendor struct {
	Name       string
	NewFetcher func() Fetcher
}

// You can BYO 3rd party status API by implementing this interface.
// Note Google Cloud AI has a dedicated implementation due to its complexity.
var Vendors = []Vendor{
	{Name: "Claude", NewFetcher: func() Fetcher { return NewClient("https://status.claude.com/api/v2") }},
	{Name: "OpenAI", NewFetcher: func() Fetcher { return NewClient("https://status.openai.com/api/v2") }},
	{Name: "Google Cloud AI", NewFetcher: func() Fetcher { return NewGoogleCloudClient() }},
	{Name: "AWS", NewFetcher: func() Fetcher { return NewAWSClient() }},
	{Name: "Cohere", NewFetcher: func() Fetcher { return NewClient("https://status.cohere.com/api/v2") }},
	{Name: "GitHub", NewFetcher: func() Fetcher { return NewClient("https://www.githubstatus.com/api/v2") }},
	{Name: "Vercel", NewFetcher: func() Fetcher { return NewClient("https://www.vercel-status.com/api/v2") }},
}
