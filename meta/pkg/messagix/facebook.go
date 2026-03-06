package messagix

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/go-querystring/query"
	"go.mau.fi/util/jsonbytes"

	"go.mau.fi/mautrix-meta/pkg/messagix/types"
)

type FacebookMethods struct {
	client *Client
}

type PushKeys struct {
	P256DH jsonbytes.UnpaddedURLBytes `json:"p256dh"`
	Auth   jsonbytes.UnpaddedURLBytes `json:"auth"`
}

func (fb *FacebookMethods) RegisterPushNotifications(ctx context.Context, endpoint string, keys PushKeys) error {
	c := fb.client
	jsonKeys, err := json.Marshal(&keys)
	if err != nil {
		c.Logger.Err(err).Msg("failed to encode push keys to json")
		return err
	}

	payload := c.newHTTPQuery()
	payload.AppID = "1443096165982425"
	payload.PushEndpoint = endpoint
	payload.SubscriptionKeys = string(jsonKeys)

	form, err := query.Values(payload)
	if err != nil {
		return err
	}

	payloadBytes := []byte(form.Encode())

	headers := c.buildHeaders(true, false)
	headers.Set("Referer", c.GetEndpoint("base_url"))
	headers.Set("Sec-fetch-site", "same-origin")
	headers.Set("Accept", "*/*")

	url := c.GetEndpoint("web_push")

	resp, body, err := c.MakeRequest(ctx, url, "POST", headers, payloadBytes, types.FORM)
	if err != nil {
		return err
	}

	if resp.StatusCode >= 300 || resp.StatusCode < 200 {
		return fmt.Errorf("bad status code: %d", resp.StatusCode)
	}

	body = bytes.TrimPrefix(body, antiJSPrefix)

	var r pushNotificationsResponse
	err = json.Unmarshal(body, &r)
	if err != nil {
		c.Logger.Err(err).Bytes("body", body).Msg("failed to unmarshal response")
		return err
	}

	if !r.Payload.Success {
		c.Logger.Err(err).Bytes("body", body).Msg("non-success push registration response")
		return errors.New("non-success response payload")
	}

	return nil
}

type pushNotificationsResponse struct {
	Ar        int     `json:"__ar"`
	Payload   payload `json:"payload"`
	DtsgToken string  `json:"dtsgToken"`
	Lid       string  `json:"lid"`
}

type payload struct {
	Success bool `json:"success"`
}

// ---------------------------------------------------------------------------
// Additional Facebook-specific HTTP helpers (ported from JS FCA)
// ---------------------------------------------------------------------------

// HandleMessageRequest accepts or rejects a message request.
// If accept is true the thread is moved to the inbox; otherwise it stays
// in the "message requests" folder.
//
// JS FCA equivalent: handleMessageRequest (POST /ajax/mercury/move_thread.php)
func (fb *FacebookMethods) HandleMessageRequest(ctx context.Context, threadID int64, accept bool) error {
	c := fb.client

	formPayload := c.newHTTPQuery()
	form, err := query.Values(formPayload)
	if err != nil {
		return fmt.Errorf("handle message request: %w", err)
	}

	form.Set("client", "mercury")
	tidStr := strconv.FormatInt(threadID, 10)
	if accept {
		form.Set("inbox[0]", tidStr)
	} else {
		form.Set("other[0]", tidStr)
	}

	headers := c.buildHeaders(true, false)
	headers.Set("referer", c.GetEndpoint("messages")+"/")
	headers.Set("origin", c.GetEndpoint("base_url"))
	headers.Set("sec-fetch-dest", "empty")
	headers.Set("sec-fetch-mode", "cors")
	headers.Set("sec-fetch-site", "same-origin")

	url := c.GetEndpoint("base_url") + "/ajax/mercury/move_thread.php"
	payloadBytes := []byte(form.Encode())

	resp, body, err := c.MakeRequest(ctx, url, "POST", headers, payloadBytes, types.FORM)
	if err != nil {
		return fmt.Errorf("handle message request: %w", err)
	}
	if resp.StatusCode >= 300 || resp.StatusCode < 200 {
		return fmt.Errorf("handle message request: bad status %d, body: %s", resp.StatusCode, string(body))
	}
	return nil
}

// MarkAllRead marks all threads in the inbox as read.
//
// JS FCA equivalent: markAsRead (POST /ajax/mercury/mark_folder_as_read.php)
func (fb *FacebookMethods) MarkAllRead(ctx context.Context) error {
	c := fb.client

	formPayload := c.newHTTPQuery()
	form, err := query.Values(formPayload)
	if err != nil {
		return fmt.Errorf("mark all read: %w", err)
	}

	form.Set("folder", "inbox")

	headers := c.buildHeaders(true, false)
	headers.Set("referer", c.GetEndpoint("messages")+"/")
	headers.Set("origin", c.GetEndpoint("base_url"))
	headers.Set("sec-fetch-dest", "empty")
	headers.Set("sec-fetch-mode", "cors")
	headers.Set("sec-fetch-site", "same-origin")

	url := c.GetEndpoint("base_url") + "/ajax/mercury/mark_folder_as_read.php"
	payloadBytes := []byte(form.Encode())

	resp, body, err := c.MakeRequest(ctx, url, "POST", headers, payloadBytes, types.FORM)
	if err != nil {
		return fmt.Errorf("mark all read: %w", err)
	}
	if resp.StatusCode >= 300 || resp.StatusCode < 200 {
		return fmt.Errorf("mark all read: bad status %d, body: %s", resp.StatusCode, string(body))
	}
	return nil
}

// ResolvePhotoURL resolves the full-size URL of a photo attachment.
//
// JS FCA equivalent: resolvePhotoUrl (GET /mercury/attachments/photo)
func (fb *FacebookMethods) ResolvePhotoURL(ctx context.Context, photoID string) (string, error) {
	c := fb.client

	formPayload := c.newHTTPQuery()
	qv, err := query.Values(formPayload)
	if err != nil {
		return "", fmt.Errorf("resolve photo url: %w", err)
	}
	qv.Set("photo_id", photoID)

	headers := c.buildHeaders(true, false)
	headers.Set("referer", c.GetEndpoint("messages")+"/")
	headers.Set("sec-fetch-dest", "empty")
	headers.Set("sec-fetch-mode", "cors")
	headers.Set("sec-fetch-site", "same-origin")

	url := c.GetEndpoint("base_url") + "/mercury/attachments/photo?" + qv.Encode()

	_, body, err := c.MakeRequest(ctx, url, "GET", headers, nil, types.NONE)
	if err != nil {
		return "", fmt.Errorf("resolve photo url: %w", err)
	}

	body = bytes.TrimPrefix(body, antiJSPrefix)

	var result struct {
		JsMods struct {
			Require [][]json.RawMessage `json:"require"`
		} `json:"jsmods"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("resolve photo url: parse response: %w", err)
	}

	// The response wraps the URL inside jsmods.require entries.
	// Look for an entry that contains an image URL string.
	for _, entry := range result.JsMods.Require {
		for _, raw := range entry {
			var s string
			if json.Unmarshal(raw, &s) == nil && (len(s) > 4 && s[:4] == "http") {
				return s, nil
			}
			// Sometimes nested in an array
			var arr []interface{}
			if json.Unmarshal(raw, &arr) == nil {
				for _, v := range arr {
					if str, ok := v.(string); ok && len(str) > 4 && str[:4] == "http" {
						return str, nil
					}
				}
			}
		}
	}
	return "", fmt.Errorf("resolve photo url: no URL found in response")
}

// ---------------------------------------------------------------------------
// Archive / Block / Delivery / Seen  (HTTP POST helpers)
// ---------------------------------------------------------------------------

// ChangeArchivedStatus archives or unarchives one or more threads.
//
// JS FCA equivalent: changeArchivedStatus (POST /ajax/mercury/change_archived_status.php)
func (fb *FacebookMethods) ChangeArchivedStatus(ctx context.Context, threadIDs []int64, archive bool) error {
	c := fb.client

	formPayload := c.newHTTPQuery()
	form, err := query.Values(formPayload)
	if err != nil {
		return fmt.Errorf("change archived status: %w", err)
	}

	archiveVal := "true"
	if !archive {
		archiveVal = "false"
	}
	for _, tid := range threadIDs {
		form.Set("ids["+strconv.FormatInt(tid, 10)+"]", archiveVal)
	}

	headers := c.buildHeaders(true, false)
	headers.Set("referer", c.GetEndpoint("messages")+"/")
	headers.Set("origin", c.GetEndpoint("base_url"))
	headers.Set("sec-fetch-dest", "empty")
	headers.Set("sec-fetch-mode", "cors")
	headers.Set("sec-fetch-site", "same-origin")

	url := c.GetEndpoint("base_url") + "/ajax/mercury/change_archived_status.php"
	resp, body, err := c.MakeRequest(ctx, url, "POST", headers, []byte(form.Encode()), types.FORM)
	if err != nil {
		return fmt.Errorf("change archived status: %w", err)
	}
	if resp.StatusCode >= 300 || resp.StatusCode < 200 {
		return fmt.Errorf("change archived status: bad status %d, body: %s", resp.StatusCode, string(body))
	}
	return nil
}

// ChangeBlockedStatus blocks or unblocks a user's messages.
//
// JS FCA equivalent: changeBlockedStatus (POST /messaging/block_messages/ or /messaging/unblock_messages/)
func (fb *FacebookMethods) ChangeBlockedStatus(ctx context.Context, userID int64, block bool) error {
	c := fb.client

	formPayload := c.newHTTPQuery()
	form, err := query.Values(formPayload)
	if err != nil {
		return fmt.Errorf("change blocked status: %w", err)
	}

	form.Set("fbid", strconv.FormatInt(userID, 10))

	headers := c.buildHeaders(true, false)
	headers.Set("referer", c.GetEndpoint("messages")+"/")
	headers.Set("origin", c.GetEndpoint("base_url"))
	headers.Set("sec-fetch-dest", "empty")
	headers.Set("sec-fetch-mode", "cors")
	headers.Set("sec-fetch-site", "same-origin")

	action := "unblock_messages"
	if block {
		action = "block_messages"
	}
	url := c.GetEndpoint("base_url") + "/messaging/" + action + "/"
	resp, body, err := c.MakeRequest(ctx, url, "POST", headers, []byte(form.Encode()), types.FORM)
	if err != nil {
		return fmt.Errorf("change blocked status: %w", err)
	}
	if resp.StatusCode >= 300 || resp.StatusCode < 200 {
		return fmt.Errorf("change blocked status: bad status %d, body: %s", resp.StatusCode, string(body))
	}
	return nil
}

// MarkAsDelivered sends a delivery receipt for a message.
//
// JS FCA equivalent: markAsDelivered (POST /ajax/mercury/delivery_receipts.php)
func (fb *FacebookMethods) MarkAsDelivered(ctx context.Context, threadID int64, messageID string) error {
	c := fb.client

	formPayload := c.newHTTPQuery()
	form, err := query.Values(formPayload)
	if err != nil {
		return fmt.Errorf("mark as delivered: %w", err)
	}

	form.Set("message_ids[0]", messageID)
	form.Set("thread_ids["+strconv.FormatInt(threadID, 10)+"][0]", messageID)

	headers := c.buildHeaders(true, false)
	headers.Set("referer", c.GetEndpoint("messages")+"/")
	headers.Set("origin", c.GetEndpoint("base_url"))
	headers.Set("sec-fetch-dest", "empty")
	headers.Set("sec-fetch-mode", "cors")
	headers.Set("sec-fetch-site", "same-origin")

	url := c.GetEndpoint("base_url") + "/ajax/mercury/delivery_receipts.php"
	resp, body, err := c.MakeRequest(ctx, url, "POST", headers, []byte(form.Encode()), types.FORM)
	if err != nil {
		return fmt.Errorf("mark as delivered: %w", err)
	}
	if resp.StatusCode >= 300 || resp.StatusCode < 200 {
		return fmt.Errorf("mark as delivered: bad status %d, body: %s", resp.StatusCode, string(body))
	}
	return nil
}

// MarkAsSeen marks all messages as seen up to a given timestamp.
//
// JS FCA equivalent: markAsSeen (POST /ajax/mercury/mark_seen.php)
func (fb *FacebookMethods) MarkAsSeen(ctx context.Context, timestampMs int64) error {
	c := fb.client

	formPayload := c.newHTTPQuery()
	form, err := query.Values(formPayload)
	if err != nil {
		return fmt.Errorf("mark as seen: %w", err)
	}

	form.Set("seen_timestamp", strconv.FormatInt(timestampMs, 10))

	headers := c.buildHeaders(true, false)
	headers.Set("referer", c.GetEndpoint("messages")+"/")
	headers.Set("origin", c.GetEndpoint("base_url"))
	headers.Set("sec-fetch-dest", "empty")
	headers.Set("sec-fetch-mode", "cors")
	headers.Set("sec-fetch-site", "same-origin")

	url := c.GetEndpoint("base_url") + "/ajax/mercury/mark_seen.php"
	resp, body, err := c.MakeRequest(ctx, url, "POST", headers, []byte(form.Encode()), types.FORM)
	if err != nil {
		return fmt.Errorf("mark as seen: %w", err)
	}
	if resp.StatusCode >= 300 || resp.StatusCode < 200 {
		return fmt.Errorf("mark as seen: bad status %d, body: %s", resp.StatusCode, string(body))
	}
	return nil
}

// SearchForThread searches for threads by name query.
//
// JS FCA equivalent: searchForThread (POST /ajax/mercury/search_threads.php)
func (fb *FacebookMethods) SearchForThread(ctx context.Context, queryStr string) (json.RawMessage, error) {
	c := fb.client

	formPayload := c.newHTTPQuery()
	form, err := query.Values(formPayload)
	if err != nil {
		return nil, fmt.Errorf("search for thread: %w", err)
	}

	form.Set("client", "web_messenger")
	form.Set("query", queryStr)
	form.Set("offset", "0")
	form.Set("limit", "21")
	form.Set("index", "fbid")

	headers := c.buildHeaders(true, false)
	headers.Set("referer", c.GetEndpoint("messages")+"/")
	headers.Set("origin", c.GetEndpoint("base_url"))
	headers.Set("sec-fetch-dest", "empty")
	headers.Set("sec-fetch-mode", "cors")
	headers.Set("sec-fetch-site", "same-origin")

	url := c.GetEndpoint("base_url") + "/ajax/mercury/search_threads.php"
	_, body, err := c.MakeRequest(ctx, url, "POST", headers, []byte(form.Encode()), types.FORM)
	if err != nil {
		return nil, fmt.Errorf("search for thread: %w", err)
	}

	body = bytes.TrimPrefix(body, antiJSPrefix)
	return json.RawMessage(body), nil
}

// GetThreadPictures fetches shared photos from a thread.
//
// JS FCA equivalent: getThreadPictures (POST /ajax/messaging/attachments/sharedphotos.php)
func (fb *FacebookMethods) GetThreadPictures(ctx context.Context, threadID int64, offset, limit int) (json.RawMessage, error) {
	c := fb.client

	formPayload := c.newHTTPQuery()
	form, err := query.Values(formPayload)
	if err != nil {
		return nil, fmt.Errorf("get thread pictures: %w", err)
	}

	form.Set("thread_id", strconv.FormatInt(threadID, 10))
	form.Set("offset", strconv.Itoa(offset))
	form.Set("limit", strconv.Itoa(limit))

	headers := c.buildHeaders(true, false)
	headers.Set("referer", c.GetEndpoint("messages")+"/")
	headers.Set("origin", c.GetEndpoint("base_url"))
	headers.Set("sec-fetch-dest", "empty")
	headers.Set("sec-fetch-mode", "cors")
	headers.Set("sec-fetch-site", "same-origin")

	url := c.GetEndpoint("base_url") + "/ajax/messaging/attachments/sharedphotos.php"
	_, body, err := c.MakeRequest(ctx, url, "POST", headers, []byte(form.Encode()), types.FORM)
	if err != nil {
		return nil, fmt.Errorf("get thread pictures: %w", err)
	}

	body = bytes.TrimPrefix(body, antiJSPrefix)
	return json.RawMessage(body), nil
}

// ---------------------------------------------------------------------------
// GraphQL helpers
// ---------------------------------------------------------------------------

// fbGraphQLPost is a small helper that posts a GraphQL request with the
// standard set of headers and returns the raw JSON body (minus the for(;;); prefix).
func (fb *FacebookMethods) fbGraphQLPost(ctx context.Context, form map[string]string) (json.RawMessage, error) {
	c := fb.client

	baseForm := c.newHTTPQuery()
	qv, err := query.Values(baseForm)
	if err != nil {
		return nil, err
	}
	for k, v := range form {
		qv.Set(k, v)
	}

	headers := c.buildHeaders(true, false)
	if fn, ok := form["fb_api_req_friendly_name"]; ok {
		headers.Set("x-fb-friendly-name", fn)
	}
	headers.Set("sec-fetch-dest", "empty")
	headers.Set("sec-fetch-mode", "cors")
	headers.Set("sec-fetch-site", "same-origin")
	headers.Set("origin", c.GetEndpoint("base_url"))
	headers.Set("referer", c.GetEndpoint("messages")+"/")

	url := c.GetEndpoint("graphql")
	resp, body, err := c.MakeRequest(ctx, url, "POST", headers, []byte(qv.Encode()), types.FORM)
	if err != nil {
		return nil, err
	}
	if resp != nil {
		c.cookies.UpdateFromResponse(resp)
	}
	body = bytes.TrimPrefix(body, antiJSPrefix)
	return json.RawMessage(body), nil
}

// GetUserInfo fetches detailed user info via the primary GraphQL batch doc.
//
// JS FCA equivalent: getUserInfo (GraphQL batch, doc_id 5009315269112105)
func (fb *FacebookMethods) GetUserInfo(ctx context.Context, userIDs []int64) (json.RawMessage, error) {
	ids := make([]string, len(userIDs))
	for i, id := range userIDs {
		ids[i] = strconv.FormatInt(id, 10)
	}

	variablesJSON, err := json.Marshal(ids)
	if err != nil {
		return nil, fmt.Errorf("get user info: %w", err)
	}

	return fb.fbGraphQLPost(ctx, map[string]string{
		"fb_api_caller_class":        "RelayModern",
		"fb_api_req_friendly_name":   "MessengerParticipantsFetcher",
		"doc_id":                     "5009315269112105",
		"server_timestamps":          "true",
		"variables":                  string(variablesJSON),
	})
}

// GetUserInfoV2 fetches user info via the CometHovercard GraphQL query.
//
// JS FCA equivalent: getUserInfoV2 (GraphQL, doc_id 24418640587785718)
func (fb *FacebookMethods) GetUserInfoV2(ctx context.Context, userID int64) (json.RawMessage, error) {
	variables := map[string]interface{}{
		"actionBarRenderLocation": "WWW_COMET_HOVERCARD",
		"context":                 "DEFAULT",
		"entityID":                strconv.FormatInt(userID, 10),
		"scale":                   1,
		"__relay_internal__pv__WorkCometIsEmployeeGKProviderrelayprovider": false,
	}
	variablesJSON, err := json.Marshal(variables)
	if err != nil {
		return nil, fmt.Errorf("get user info v2: %w", err)
	}

	return fb.fbGraphQLPost(ctx, map[string]string{
		"fb_api_caller_class":        "RelayModern",
		"fb_api_req_friendly_name":   "CometHovercardQueryRendererQuery",
		"doc_id":                     "24418640587785718",
		"server_timestamps":          "true",
		"variables":                  string(variablesJSON),
	})
}

// CreateThemeAI generates an AI-designed chat theme from a text prompt.
//
// JS FCA equivalent: createThemeAI (GraphQL, doc_id 23873748445608673)
func (fb *FacebookMethods) CreateThemeAI(ctx context.Context, prompt string, actorID int64) (json.RawMessage, error) {
	variables := map[string]interface{}{
		"input": map[string]interface{}{
			"client_mutation_id": strconv.Itoa(int(actorID % 20)),
			"actor_id":          strconv.FormatInt(actorID, 10),
			"bypass_cache":      true,
			"caller":            "MESSENGER",
			"num_themes":        1,
			"prompt":            prompt,
		},
	}
	variablesJSON, err := json.Marshal(variables)
	if err != nil {
		return nil, fmt.Errorf("create theme ai: %w", err)
	}

	return fb.fbGraphQLPost(ctx, map[string]string{
		"fb_api_caller_class":        "RelayModern",
		"fb_api_req_friendly_name":   "useGenerateAIThemeMutation",
		"doc_id":                     "23873748445608673",
		"server_timestamps":          "true",
		"variables":                  string(variablesJSON),
	})
}

// GetThemePictures fetches theme pictures/assets for a given theme ID.
//
// JS FCA equivalent: getThemePictures (GraphQL, doc_id 9734829906576883)
func (fb *FacebookMethods) GetThemePictures(ctx context.Context, themeID string) (json.RawMessage, error) {
	variables := map[string]interface{}{"id": themeID}
	variablesJSON, err := json.Marshal(variables)
	if err != nil {
		return nil, fmt.Errorf("get theme pictures: %w", err)
	}

	return fb.fbGraphQLPost(ctx, map[string]string{
		"fb_api_caller_class":        "RelayModern",
		"fb_api_req_friendly_name":   "MWPThreadThemeProviderQuery",
		"doc_id":                     "9734829906576883",
		"server_timestamps":          "true",
		"variables":                  string(variablesJSON),
	})
}

// SetPostReaction sets a reaction on a Facebook post.
//
// JS FCA equivalent: setPostReaction (GraphQL, doc_id 4769042373179384)
//
// Reaction types: 0=unlike, 1=like, 2=heart, 16=love, 4=haha, 3=wow, 7=sad, 8=angry
func (fb *FacebookMethods) SetPostReaction(ctx context.Context, postID string, reactionType int, actorID int64) (json.RawMessage, error) {
	// Facebook expects base64-encoded "feedback:<postID>"
	feedbackID := "feedback:" + postID

	variables := map[string]interface{}{
		"input": map[string]interface{}{
			"actor_id":               strconv.FormatInt(actorID, 10),
			"feedback_id":            feedbackID,
			"feedback_reaction":      reactionType,
			"feedback_source":        "OBJECT",
			"is_tracking_encrypted":  true,
			"tracking":               []string{},
			"session_id":             "00000000-0000-0000-0000-000000000000",
			"client_mutation_id":     strconv.Itoa(int(actorID % 20)),
		},
		"useDefaultActor": false,
		"scale":           3,
	}
	variablesJSON, err := json.Marshal(variables)
	if err != nil {
		return nil, fmt.Errorf("set post reaction: %w", err)
	}

	return fb.fbGraphQLPost(ctx, map[string]string{
		"fb_api_caller_class":        "RelayModern",
		"fb_api_req_friendly_name":   "CometUFIFeedbackReactMutation",
		"doc_id":                     "4769042373179384",
		"server_timestamps":          "true",
		"variables":                  string(variablesJSON),
	})
}

// ---------------------------------------------------------------------------
// Friend / Social actions
// ---------------------------------------------------------------------------

// HandleFriendRequest accepts or rejects a pending friend request.
//
// JS FCA equivalent: handleFriendRequest (POST /requests/friends/ajax/)
func (fb *FacebookMethods) HandleFriendRequest(ctx context.Context, userID int64, accept bool) error {
	c := fb.client

	formPayload := c.newHTTPQuery()
	form, err := query.Values(formPayload)
	if err != nil {
		return fmt.Errorf("handle friend request: %w", err)
	}

	form.Set("viewer_id", c.configs.BrowserConfigTable.CurrentUserInitialData.UserID)
	form.Set("frefs[0]", "jwl")
	form.Set("floc", "friend_center_requests")
	form.Set("ref", "/reqs.php")
	if accept {
		form.Set("action", "confirm")
	} else {
		form.Set("action", "reject")
	}
	form.Set("id", strconv.FormatInt(userID, 10))

	headers := c.buildHeaders(true, false)
	headers.Set("referer", c.GetEndpoint("base_url")+"/friends/")
	headers.Set("origin", c.GetEndpoint("base_url"))
	headers.Set("sec-fetch-dest", "empty")
	headers.Set("sec-fetch-mode", "cors")
	headers.Set("sec-fetch-site", "same-origin")

	url := c.GetEndpoint("base_url") + "/requests/friends/ajax/"
	resp, body, err := c.MakeRequest(ctx, url, "POST", headers, []byte(form.Encode()), types.FORM)
	if err != nil {
		return fmt.Errorf("handle friend request: %w", err)
	}
	if resp.StatusCode >= 300 || resp.StatusCode < 200 {
		return fmt.Errorf("handle friend request: bad status %d, body: %s", resp.StatusCode, string(body))
	}
	return nil
}

// Unfriend removes a user from the friends list.
//
// JS FCA equivalent: unfriend (POST /ajax/profile/removefriendconfirm.php)
func (fb *FacebookMethods) Unfriend(ctx context.Context, userID int64) error {
	c := fb.client

	formPayload := c.newHTTPQuery()
	form, err := query.Values(formPayload)
	if err != nil {
		return fmt.Errorf("unfriend: %w", err)
	}

	form.Set("uid", strconv.FormatInt(userID, 10))
	form.Set("unref", "bd_friends_tab")
	form.Set("floc", "friends_tab")
	form.Set("nctr[_mod]", "pagelet_timeline_app_collection_"+c.configs.BrowserConfigTable.CurrentUserInitialData.UserID+":2356318349:2")

	headers := c.buildHeaders(true, false)
	headers.Set("referer", c.GetEndpoint("base_url")+"/friends/")
	headers.Set("origin", c.GetEndpoint("base_url"))
	headers.Set("sec-fetch-dest", "empty")
	headers.Set("sec-fetch-mode", "cors")
	headers.Set("sec-fetch-site", "same-origin")

	url := c.GetEndpoint("base_url") + "/ajax/profile/removefriendconfirm.php"
	resp, body, err := c.MakeRequest(ctx, url, "POST", headers, []byte(form.Encode()), types.FORM)
	if err != nil {
		return fmt.Errorf("unfriend: %w", err)
	}
	if resp.StatusCode >= 300 || resp.StatusCode < 200 {
		return fmt.Errorf("unfriend: bad status %d, body: %s", resp.StatusCode, string(body))
	}
	return nil
}
