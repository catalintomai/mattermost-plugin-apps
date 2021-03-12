package restapi

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mattermost/mattermost-plugin-apps/apps"
	"github.com/mattermost/mattermost-plugin-apps/server/api"
	"github.com/mattermost/mattermost-plugin-apps/server/utils/httputils"

	"github.com/pkg/errors"
)

func (a *restapi) handleGetBindings(w http.ResponseWriter, req *http.Request, actingUserID string) {
	sessionID := req.Header.Get("MM_SESSION_ID")
	if sessionID == "" {
		err := errors.New("no user session")
		httputils.WriteUnauthorizedError(w, err)
		return
	}
	session, err := a.api.Mattermost.Session.Get(sessionID)
	if err != nil {
		httputils.WriteUnauthorizedError(w, err)
		return
	}

	query := req.URL.Query()
	bindings, err := a.api.Proxy.GetBindings(apps.SessionToken(session.Token),
		&apps.Context{
			TeamID:            query.Get(api.PropTeamID),
			ChannelID:         query.Get(api.PropChannelID),
			ActingUserID:      actingUserID,
			UserID:            actingUserID,
			PostID:            query.Get(api.PropPostID),
			UserAgent:         query.Get(api.PropUserAgent),
			MattermostSiteURL: a.api.Configurator.GetConfig().MattermostSiteURL,
		})
	if err != nil {
		httputils.WriteInternalServerError(w, err)
		return
	}

	httputils.WriteJSON(w, bindings)
}

func (a *restapi) handleInvalidateCache(w http.ResponseWriter, req *http.Request, actingUserID string) {
	vars := mux.Vars(req)

	appID := vars["app_id"]
	userID := vars["user_id"]
	channelID := vars["channel_id"]

	if appID == "" {
		httputils.WriteBadRequestError(w, errors.New("app_id not specified"))
		return
	}

	if err := a.api.Proxy.InvalidateCache(apps.AppID(appID), userID, channelID); err != nil {
		httputils.WriteInternalServerError(w, err)
	}
}