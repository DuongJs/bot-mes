package media

type MediaType string

const (
	Image MediaType = "image"
	Video MediaType = "video"
)

type MediaItem struct {
	Type MediaType
	URL  string
}

// MediaResult holds the result of a media fetch, including optional post text.
type MediaResult struct {
	Message string      // Post text content (may be empty)
	Items   []MediaItem // Media items (images, videos)
}
