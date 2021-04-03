package proxy

import (
	"encoding/json"
	"fmt"
	"sync"
	"unicode/utf8"

	"github.com/mattermost/mattermost-plugin-apps/apps"
	"github.com/mattermost/mattermost-plugin-apps/server/store"
	"github.com/pkg/errors"
)

const (
	KEY_VALUE_KEY_MAX_RUNES       = 50
	KEY_ALL_USERS                 = "ALL_USERS"
	KEY_ALL_CHANNELS              = "ALL_CHANNELS"
)

func mergeBindings(bb1, bb2 []*apps.Binding) []*apps.Binding {
	out := append([]*apps.Binding(nil), bb1...)

	for _, b2 := range bb2 {
		found := false
		for i, o := range out {
			if b2.AppID == o.AppID && b2.Location == o.Location {
				found = true

				// b2 overrides b1, if b1 and b2 have Bindings, they are merged
				merged := b2
				if len(o.Bindings) != 0 && b2.Call == nil {
					merged.Bindings = mergeBindings(o.Bindings, b2.Bindings)
				}
				out[i] = merged
			}
		}
		if !found {
			out = append(out, b2)
		}
	}
	return out
}

// GetBindings fetches bindings for all apps.
// We should avoid unnecessary logging here as this route is called very often.
func (p *Proxy) GetBindings(sessionID, actingUserID string, cc *apps.Context) ([]*apps.Binding, error) {
	allApps := store.SortApps(p.store.App.AsMap())

	all := make([][]*apps.Binding, len(allApps))
	var wg sync.WaitGroup

	for i, app := range allApps {
		wg.Add(1)

		go func(sessionID, actingUserID string, cc *apps.Context, app *apps.App, i int) {
			defer wg.Done()

			bindings, err := p.GetBindingsForApp(sessionID, actingUserID, cc, app)
			if err != nil {
				p.mm.Log.Debug("Failed to get binding for app", "error", err.Error(), "appID", app.AppID)
				return
			}

			all[i] = bindings
		}(sessionID, actingUserID, cc, app, i)
	}

	wg.Wait()

	ret := []*apps.Binding{}
	for _, b := range all {
		ret = mergeBindings(ret, b)
	}

	return ret, nil
}

// GetBindingsForApp fetches bindings for a specific apps.
// We should avoid unnecessary logging here as this route is called very often.
func (p *Proxy) GetBindingsForApp(sessionID, actingUserID string, cc *apps.Context, app *apps.App) ([]*apps.Binding, error) {
	if !p.AppIsEnabled(app) {
		return nil, nil
	}

	appID := app.AppID
	appCC := *cc
	appCC.AppID = appID
	appCC.BotAccessToken = app.BotAccessToken

	var err error
	var bindings = []*apps.Binding{}
	bindings, err = p.CacheGetAll(cc, appID);
	if err != nil || len(bindings) == 0 {
		bindingsCall := apps.DefaultBindings.WithOverrides(app.Bindings)
		bindingsRequest := &apps.CallRequest{
			Call:    *bindingsCall,
			Context: &appCC,
		}

		resp := p.Call(sessionID, actingUserID, bindingsRequest)
		if resp == nil || resp.Type != apps.CallResponseTypeOK {
			// TODO Log error (chance to flood the logs)
			// p.mm.Log.Debug("Response is nil or unexpected type.")
			// if resp != nil && resp.Type == apps.CallResponseTypeError {
			// 	p.mm.Log.Debug("Error getting bindings. Error: " + resp.Error())
			// }
			return nil, nil
		}

		b, _ := json.Marshal(resp.Data)
		err := json.Unmarshal(b, &bindings)
		if err == nil {
			if storeErr := p.CacheSet(cc, appID, bindings); storeErr != nil { // store the bindings to the cache
				p.mm.Log.Error(fmt.Sprintf("failed to store bindings to cache for %s: %v", appID, storeErr))
			}
		} else {
			// TODO Log error (chance to flood the logs)
			// p.mm.Log.Debug("Bindings are not of the right type.")
		}
	}

	bindings = p.scanAppBindings(app, bindings, "")

	return bindings, nil
}

// scanAppBindings removes bindings to locations that have not been granted to
// the App, and sets the AppID on the relevant elements.
func (p *Proxy) scanAppBindings(app *apps.App, bindings []*apps.Binding, locPrefix apps.Location) []*apps.Binding {
	out := []*apps.Binding{}
	locationsUsed := map[apps.Location]bool{}
	labelsUsed := map[string]bool{}

	for _, appB := range bindings {
		// clone just in case
		b := *appB
		if b.Location == "" {
			b.Location = apps.Location(app.Manifest.AppID)
		}

		fql := locPrefix.Make(b.Location)
		allowed := false
		for _, grantedLoc := range app.GrantedLocations {
			if fql.In(grantedLoc) || grantedLoc.In(fql) {
				allowed = true
				break
			}
		}
		if !allowed {
			//p.mm.Log.Debug(fmt.Sprintf("location %s is not granted to app %s", fql, app.Manifest.AppID))
			continue
		}

		if locPrefix == apps.LocationCommand {
			b.Location = apps.Location(app.Manifest.AppID)
			b.Label = string(app.Manifest.AppID)
		}

		if fql.IsTop() {
			if locationsUsed[appB.Location] {
				continue
			}
			locationsUsed[appB.Location] = true
		} else {
			if b.Location == "" || b.Label == "" {
				continue
			}
			if locationsUsed[appB.Location] || labelsUsed[appB.Label] {
				continue
			}

			locationsUsed[appB.Location] = true
			labelsUsed[appB.Label] = true
			b.AppID = app.Manifest.AppID
		}

		if len(b.Bindings) != 0 {
			scanned := p.scanAppBindings(app, b.Bindings, fql)
			if len(scanned) == 0 {
				// We do not add bindings without any valid sub-bindings
				continue
			}
			b.Bindings = scanned
		}
		out = append(out, &b)
	}
	return out
}

func (p *Proxy) CacheSet(cc *apps.Context, appID apps.AppID, bindings []*apps.Binding) error {
	groupedBindingsMap := map[string][][]byte{}

	for _, binding := range bindings {
		userID := KEY_ALL_USERS
		if binding.DependsOnUser && cc.ActingUserID != "" {
			userID = cc.ActingUserID
		}

		channelID := KEY_ALL_CHANNELS
		if binding.DependsOnChannel && cc.ChannelID != "" {
			channelID = cc.ChannelID
		}

		valueBytes, err := json.Marshal(binding)
		if err != nil {
			return errors.Wrapf(err, "failed to marshal value")
		}

		key := p.CacheBuildKey(userID, channelID)
		bindingsForKey := groupedBindingsMap[key]
		bindingsForKey = append(bindingsForKey, valueBytes)
		groupedBindingsMap[key] = bindingsForKey
	}

	if storeErr := p.mm.AppsCache.Set(string(appID), groupedBindingsMap); storeErr != nil {
		p.mm.Log.Error(fmt.Sprintf("failed to store bindings to cache for %s: %v", appID, storeErr))
		return storeErr
	}

	return nil
}

func (p *Proxy) CacheGetAll(cc *apps.Context, appID apps.AppID) ([]*apps.Binding, error) {
	bindings := []*apps.Binding{}

	keys := p.CacheBuildKeys(cc.ActingUserID, cc.ChannelID)
	for _, key := range keys {
		tbindings, err := p.CacheGet(cc, appID, key)
		if err != nil {
			return nil, err
		}
		bindings = append(bindings, tbindings...)
	}

	return bindings, nil
}

func (p *Proxy) CacheGet(cc *apps.Context, appID apps.AppID, key string) ([]*apps.Binding, error) {
	bindings := []*apps.Binding{}

	var retErr error
	if outBindings, err := p.mm.AppsCache.Get(string(appID), key); err == nil {
		b := apps.Binding{}
		for _, outBinding := range outBindings {
			if err := json.Unmarshal(outBinding, &b); err != nil {
				p.mm.Log.Error(fmt.Sprintf("failed to unmarshal value for key %s", key))
				retErr = err
				break
			}
			bindings = append(bindings, &b)
		}
	}

	return bindings, retErr
}

func (p *Proxy) CacheDelete(appID apps.AppID, key string) (error) {
	return p.mm.AppsCache.Delete(string(appID), key);
}

func (p *Proxy) CacheEmpty(appID apps.AppID) (error) {
	return p.mm.AppsCache.DeleteAll(string(appID));
}

func (p *Proxy) CacheEmptyApps() []error {
	errors := []error{}

	allApps := store.SortApps(p.store.App.AsMap())
	for _, app := range allApps {
		if err := p.CacheEmpty(app.Manifest.AppID); err != nil {
			errors = append(errors, err)
		}
	}

	return errors
}

func (p *Proxy) CacheBuildKeys(userID string, channelID string) []string {
	keys := []string{}

	keys = append(keys, p.CacheBuildKey(KEY_ALL_USERS, KEY_ALL_CHANNELS))

	if userID != "" {
		keys = append(keys, p.CacheBuildKey(userID, KEY_ALL_CHANNELS))
	}

	if channelID != "" {
		keys = append(keys, p.CacheBuildKey(KEY_ALL_USERS, channelID))
	}

	if userID != "" && channelID != "" {
		keys = append(keys, p.CacheBuildKey(userID, channelID))
	}
	return keys
}

func (p *Proxy) CacheBuildKey(userId string, channelId string) string {
	key := fmt.Sprintf("%s:%s", userId, channelId)

	if utf8.RuneCountInString(key) > KEY_VALUE_KEY_MAX_RUNES {
		return key[:KEY_VALUE_KEY_MAX_RUNES]
	}

	return key
}

func (p *Proxy) InvalidateCache(cc *apps.Context, appID apps.AppID) error {
	userID := cc.ActingUserID
	channelID := cc.ChannelID

	if cc.ActingUserID == "" {
		userID = KEY_ALL_USERS
	}

	if cc.ChannelID == "" {
		channelID = KEY_ALL_CHANNELS
	}

	key := p.CacheBuildKey(userID, channelID)
	return p.CacheDelete(appID, key)
}