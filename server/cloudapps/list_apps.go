// Copyright (c) 2019-present Mattermost, Inc. All Rights Reserved.
// See License for license information.

package cloudapps

import (
	"github.com/mattermost/mattermost-plugin-cloudapps/server/utils/md"
)

type OutListApps struct {
	md.MD
}

func (r *registry) ListApps() (*OutListApps, error) {
	out := &OutListApps{
		MD: "test",
	}
	return out, nil
}
