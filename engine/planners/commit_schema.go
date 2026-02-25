package planners

import "github.com/invopop/jsonschema"

// ClusterGroup represents one logical group of hunks from the clustering pass.
type ClusterGroup struct {
	ID      string   `json:"id" jsonschema_description:"Stable group identifier (g1, g2, ...)"`
	HunkIDs []string `json:"hunk_ids" jsonschema_description:"List of hunk_id values in this group"`
}

// ClusteringResponse is the schema for the clustering LLM call.
type ClusteringResponse struct {
	Groups []ClusterGroup `json:"groups" jsonschema_description:"Logical groups of related hunks"`
}

// CommitMessage is the schema for the messaging LLM call (one per group).
type CommitMessage struct {
	Type     string          `json:"type" jsonschema_description:"Conventional commit type (feat, fix, refactor, etc.)"`
	Scope    *string         `json:"scope" jsonschema_description:"Optional scope"`
	Subject  string          `json:"subject" jsonschema_description:"Imperative commit subject, no trailing period"`
	Body     *string         `json:"body" jsonschema_description:"Optional commit body with more detail"`
	Breaking bool            `json:"breaking" jsonschema_description:"True if this is a breaking change"`
	Footers  []FooterMessage `json:"footers" jsonschema_description:"Commit footers (e.g., BREAKING CHANGE)"`
}

// FooterMessage is a single commit footer in the messaging response.
type FooterMessage struct {
	Token string `json:"token" jsonschema_description:"Footer token (e.g., BREAKING CHANGE, Refs)"`
	Value string `json:"value" jsonschema_description:"Footer value"`
}

// MessagingResponse is the schema for the messaging LLM call.
type MessagingResponse struct {
	Commits []CommitMessageWithGroup `json:"commits" jsonschema_description:"One commit per cluster group"`
}

// CommitMessageWithGroup pairs a group ID with its commit metadata.
type CommitMessageWithGroup struct {
	GroupID string `json:"group_id" jsonschema_description:"The group ID this commit corresponds to"`
	CommitMessage
}

func generateSchema[T any]() interface{} {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}
	var v T
	return reflector.Reflect(v)
}

var (
	ClusteringSchema = generateSchema[ClusteringResponse]()
	MessagingSchema  = generateSchema[MessagingResponse]()
)
