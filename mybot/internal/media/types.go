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
