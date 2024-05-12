package types

// ApEntity is a db model of an ActivityPub entity.
type ApEntity struct {
	ID         string `json:"id" gorm:"type:text"`
	CCID       string `json:"ccid" gorm:"type:char(42)"`
	Publickey  string `json:"publickey" gorm:"type:text"`
	Privatekey string `json:"privatekey" gorm:"type:text"`
}

// ApPerson is a db model of an ActivityPub entity.
type ApPerson struct {
	ID      string `json:"id" gorm:"type:text"`
	Name    string `json:"name" gorm:"type:text"`
	Summary string `json:"summary" gorm:"type:text"`
	IconURL string `json:"icon_url" gorm:"type:text"`
}

// ApFollow is a db model of an ActivityPub follow.
// Concurrent -> Activitypub
type ApFollow struct {
	ID                 string `json:"id" gorm:"type:text"`
	Accepted           bool   `json:"accepted" gorm:"type:bool"`
	PublisherPersonURL string `json:"publisher" gorm:"type:text"`  // ActivityPub Person
	SubscriberUserID   string `json:"subscriber" gorm:"type:text"` // Concurrent APID
}

// ApFollwer is a db model of an ActivityPub follower.
// Activitypub -> Concurrent
type ApFollower struct {
	ID                  string `json:"id" gorm:"type:text"`
	SubscriberPersonURL string `json:"subscriber" gorm:"type:text;uniqueIndex:uniq_apfollower;"` // ActivityPub Person
	PublisherUserID     string `json:"publisher" gorm:"type:text;uniqueIndex:uniq_apfollower;"`  // Concurrent APID
	SubscriberInbox     string `json:"subscriber_inbox" gorm:"type:text"`                        // ActivityPub Inbox
}

// ApObjectReference is a db model of an ActivityPub object cross reference.
type ApObjectReference struct {
	ApObjectID string `json:"apobjectID" gorm:"primaryKey;type:text;"`
	CcObjectID string `json:"ccobjectID" gorm:"type:text;"`
}
