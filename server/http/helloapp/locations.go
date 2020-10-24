package helloapp

import (
	"io"
	"net/http"
	"strings"

	"github.com/mattermost/mattermost-plugin-apps/server/apps"
	"github.com/mattermost/mattermost-plugin-apps/server/utils/httputils"
)

const (
	sampleIcon = "http://www.mattermost.org/wp-content/uploads/2016/04/icon.png"
)

func (h *helloapp) HandleLocations(w http.ResponseWriter, req *http.Request, userID, channelID string) {
	user, err := h.apps.Mattermost.User.Get(userID)
	if err != nil {
		httputils.WriteInternalServerError(w, err)
		return
	}

	reader, err := h.apps.Mattermost.User.GetProfileImage(userID)
	if err != nil {
		httputils.WriteInternalServerError(w, err)
		return
	}
	icon := new(strings.Builder)
	_, err = io.Copy(icon, reader)
	if err != nil {
		httputils.WriteInternalServerError(w, err)
		return
	}

	locations := []apps.LocationInt{
		&apps.ChannelHeaderIconLocation{
			Location: apps.Location{
				AppID:        AppID,
				LocationType: apps.LocationChannelHeaderIcon,
				FormURL:      h.AppURL(PathPing),
			},
			DropdownText: user.Username,
			AriaText:     user.Username,
			Icon:         sampleIcon,
		},
		&apps.PostMenuItemLocation{
			Location: apps.Location{
				AppID:        AppID,
				LocationType: apps.LocationPostMenuItem,
				FormURL:      h.AppURL(PathPing),
			},
			Text: user.Username,
			Icon: sampleIcon,
		},
		&apps.PostMenuItemLocation{
			Location: apps.Location{
				AppID:        AppID,
				LocationType: apps.LocationPostMenuItem,
				FormURL:      h.AppURL(PathPing),
			},
			Text: "Remove " + user.Username,
			Icon: sampleIcon,
		},
		&apps.SlashCommandLocation{
			Location: apps.Location{
				AppID:        AppID,
				LocationType: apps.LocationSlashCommand,
				FormURL:      h.AppURL(PathCommandDefinition),
			},
			Trigger: "hello",
			Text:    "Say hello!",
			Icon:    sampleIcon,
		},
	}

	httputils.WriteJSON(w, locations)
}