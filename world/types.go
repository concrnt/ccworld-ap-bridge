package world

type Profile struct {
	Username    string   `json:"username"`
	Avatar      string   `json:"avatar"`
	Description string   `json:"description"`
	Banner      string   `json:"banner"`
	Subprofiles []string `json:"subprofiles"`
}

type Emoji struct {
	ImageURL string `json:"imageURL"`
}

type ProfileOverride struct {
	Username    string `json:"username"`
	Avatar      string `json:"avatar"`
	Description string `json:"description"`
	Link        string `json:"link"`
	CharacterID string `json:"characterID"`
}

type MarkdownMessage struct {
	Body            string           `json:"body"`
	Emojis          map[string]Emoji `json:"emojis"`
	ProfileOverride ProfileOverride  `json:"profileOverride"`
}

type ReactionAssociation struct {
	ImageURL        string          `json:"imageUrl"`
	Shortcode       string          `json:"shortcode"`
	ProfileOverride ProfileOverride `json:"profileOverride"`
}

type LikeAssociation struct {
	ProfileOverride ProfileOverride `json:"profileOverride"`
}

type ReplyAssociation struct {
	MessageID       string          `json:"messageId"`
	MessageAuthor   string          `json:"messageAuthor"`
	ProfileOverride ProfileOverride `json:"profileOverride"`
}

type ReplyMessage struct {
	ReplyToMessageID     string           `json:"replyToMessageId"`
	ReplyToMessageAuthor string           `json:"replyToMessageAuthor"`
	Body                 string           `json:"body"`
	Emojis               map[string]Emoji `json:"emojis"`
	ProfileOverride      ProfileOverride  `json:"profileOverride"`
}

type RerouteAssociation struct {
	MessageID       string          `json:"messageId"`
	MessageAuthor   string          `json:"messageAuthor"`
	ProfileOverride ProfileOverride `json:"profileOverride"`
}

type RerouteMessage struct {
	RerouteToMessageID     string           `json:"rerouteToMessageId"`
	RerouteToMessageAuthor string           `json:"rerouteToMessageAuthor"`
	Body                   string           `json:"body"`
	Emojis                 map[string]Emoji `json:"emojis"`
	ProfileOverride        ProfileOverride  `json:"profileOverride"`
}
