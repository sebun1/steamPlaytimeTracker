package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sebun1/steamPlaytimeTracker/log"
	"github.com/sebun1/steamPlaytimeTracker/sptt"
)

const (
	ClearanceNone              = 0
	ClearanceAdminBase         = 500
	ClearanceAdminModifyDelete = 600
)

// adminResp is the minimal response envelope for all admin endpoints.
type adminResp struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason"`
}

func okResp() adminResp               { return adminResp{OK: true} }
func errResp(reason string) adminResp { return adminResp{OK: false, Reason: reason} }

// clearanceFromCtx retrieves the caller's clearance stored by the middleware.
func clearanceFromCtx(c *gin.Context) int {
	v, _ := c.Get("clearance")
	cl, _ := v.(int)
	return cl
}

// checkClearance responds with bad_auth and returns false if the caller's
// clearance is below the required level.
func checkClearance(c *gin.Context, required int) bool {
	if clearanceFromCtx(c) < required {
		c.JSON(http.StatusForbidden, errResp("bad_auth"))
		log.Warnf("Unauthorized admin access attempt with clearance %d (required %d)", clearanceFromCtx(c), required)
		return false
	}
	return true
}

func parseAdminSteamID(raw string) (sptt.SteamID, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	v, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || v == 0 {
		return 0, false
	}
	return sptt.SteamID(v), true
}

// ── Middleware ────────────────────────────────────────────────────────────────

// AdminAuthMiddleware authenticates every request in the /admin group using
// X-Admin-Name and X-Admin-Token headers.
func AdminAuthMiddleware(db *sptt.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.GetHeader("X-Admin-Name")
		token := c.GetHeader("X-Admin-Token")

		clearance, ok := sptt.Authenticate(db, name, token)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, errResp("bad_auth"))
			return
		}
		c.Set("clearance", clearance)
		c.Next()
	}
}

// ── Reload Helper ─────────────────────────────────────────────────────────────

// reloadActiveUsers pushes a UserListUpdate notification on the channel and
// stamps the metadata with the current time.
func reloadActiveUsers(a *SptAPI) error {
	select {
	case a.notifChan <- sptt.NotifUserListUpdate():
	default:
		// Channel full — the monitor will still pick up the reload on its next
		// tick, so this is not fatal.
	}
	return a.db.SetMetadata(a.ctx, sptt.MetaKeyLastUserReload, time.Now().UTC().Format(time.RFC3339))
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// GET /admin/test
func (a *SptAPI) handleAdminTest(c *gin.Context) {
	if !checkClearance(c, ClearanceNone) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "reason": "", "clearance": clearanceFromCtx(c)})
}

// POST /admin/reload
func (a *SptAPI) handleAdminReload(c *gin.Context) {
	if !checkClearance(c, ClearanceAdminModifyDelete) {
		return
	}
	if err := reloadActiveUsers(a); err != nil {
		c.JSON(http.StatusInternalServerError, errResp("internal_error"))
		return
	}
	c.JSON(http.StatusOK, okResp())
}

// GET /admin/users?limit=50&offset=0
func (a *SptAPI) handleAdminGetUsers(c *gin.Context) {
	if !checkClearance(c, ClearanceAdminBase) {
		return
	}

	limit := 50
	offset := 0
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if v := c.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	users, total, err := a.db.GetUsers(a.ctx, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errResp("internal_error"))
		return
	}

	lastReload, _ := a.db.GetMetadata(a.ctx, sptt.MetaKeyLastUserReload)

	type userRow struct {
		SteamID  string `json:"steamid"`
		Username string `json:"username"`
		Active   bool   `json:"active"`
		Public   bool   `json:"public"`
	}

	rows := make([]userRow, 0, len(users))
	for _, u := range users {
		nextRow := userRow{
			SteamID:  strconv.FormatUint(uint64(u.SteamID), 10),
			Username: u.Username,
			Active:   u.Active,
			Public:   u.Public,
		}
		rows = append(rows, nextRow)
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":          true,
		"reason":      "",
		"users":       rows,
		"total":       total,
		"last_reload": lastReload,
	})
}

// POST /admin/users/add
func (a *SptAPI) handleAdminAddUser(c *gin.Context) {
	if !checkClearance(c, ClearanceAdminBase) {
		return
	}

	var body struct {
		SteamID  string `json:"steamid"`
		Username string `json:"username"`
		Active   *bool  `json:"active"`
		Public   *bool  `json:"public"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.Username) == "" {
		c.JSON(http.StatusBadRequest, errResp("bad_request"))
		return
	}
	id, ok := parseAdminSteamID(body.SteamID)
	if !ok {
		c.JSON(http.StatusBadRequest, errResp("bad_request"))
		return
	}

	active := true
	public := true
	if body.Active != nil {
		active = *body.Active
	}
	if body.Public != nil {
		public = *body.Public
	}

	err := a.db.AddUser(a.ctx, id, strings.TrimSpace(body.Username), active, public)
	if err != nil {
		if errors.Is(err, sptt.ErrDuplicateSteamID) {
			c.JSON(http.StatusConflict, errResp("duplicate_steamid"))
			return
		}
		c.JSON(http.StatusInternalServerError, errResp("internal_error"))
		return
	}

	_ = reloadActiveUsers(a)
	c.JSON(http.StatusOK, okResp())
}

// POST /admin/users/remove
func (a *SptAPI) handleAdminRemoveUser(c *gin.Context) {
	if !checkClearance(c, ClearanceAdminModifyDelete) {
		return
	}

	var body struct {
		SteamID string `json:"steamid"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, errResp("bad_request"))
		return
	}
	id, ok := parseAdminSteamID(body.SteamID)
	if !ok {
		c.JSON(http.StatusBadRequest, errResp("bad_request"))
		return
	}

	err := a.db.RemoveUser(a.ctx, id)
	if err != nil {
		if errors.Is(err, sptt.ErrUserNotFound) {
			c.JSON(http.StatusNotFound, errResp("not_found"))
			return
		}
		c.JSON(http.StatusInternalServerError, errResp("internal_error"))
		return
	}

	_ = reloadActiveUsers(a)
	c.JSON(http.StatusOK, okResp())
}

// POST /admin/users/modify
func (a *SptAPI) handleAdminModifyUser(c *gin.Context) {
	if !checkClearance(c, ClearanceAdminModifyDelete) {
		return
	}

	var body struct {
		SteamID  string  `json:"steamid"`
		Username *string `json:"username"`
		Active   *bool   `json:"active"`
		Public   *bool   `json:"public"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, errResp("bad_request"))
		return
	}
	id, ok := parseAdminSteamID(body.SteamID)
	if !ok {
		c.JSON(http.StatusBadRequest, errResp("bad_request"))
		return
	}
	if body.Username != nil {
		trimmed := strings.TrimSpace(*body.Username)
		body.Username = &trimmed
	}

	err := a.db.ModifyUser(a.ctx, id, sptt.ModifyUserParams{
		Username: body.Username,
		Active:   body.Active,
		Public:   body.Public,
	})
	if err != nil {
		if errors.Is(err, sptt.ErrUserNotFound) {
			c.JSON(http.StatusNotFound, errResp("not_found"))
			return
		}
		c.JSON(http.StatusInternalServerError, errResp("internal_error"))
		return
	}

	_ = reloadActiveUsers(a)
	c.JSON(http.StatusOK, okResp())
}

// GET /admin/tokens
func (a *SptAPI) handleAdminListTokens(c *gin.Context) {
	if !checkClearance(c, ClearanceNone) {
		return
	}

	tokens, err := a.db.ListAuthTokensBelowClearance(a.ctx, clearanceFromCtx(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, errResp("internal_error"))
		return
	}

	type tokenRow struct {
		Name       string `json:"name"`
		Clearance  int    `json:"clearance"`
		CreateDate string `json:"create_date"`
	}

	rows := make([]tokenRow, 0, len(tokens))
	for _, t := range tokens {
		rows = append(rows, tokenRow{
			Name:       t.Name,
			Clearance:  t.Clearance,
			CreateDate: t.CreateDate.UTC().Format(time.RFC3339),
		})
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "reason": "", "tokens": rows})
}

// POST /admin/tokens/create
func (a *SptAPI) handleAdminCreateToken(c *gin.Context) {
	if !checkClearance(c, ClearanceAdminModifyDelete) {
		return
	}

	var body struct {
		Name      string `json:"name"`
		Clearance int    `json:"clearance"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Name == "" {
		c.JSON(http.StatusBadRequest, errResp("bad_request"))
		return
	}

	// Requested clearance must be strictly below caller's.
	if body.Clearance >= clearanceFromCtx(c) {
		c.JSON(http.StatusForbidden, errResp("bad_clearance"))
		return
	}

	tokenHex, saltHex, secretHex, err := sptt.GenerateToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, errResp("internal_error"))
		return
	}

	if err := a.db.CreateAuthToken(a.ctx, body.Name, saltHex, secretHex, body.Clearance); err != nil {
		if errors.Is(err, sptt.ErrDuplicateTokenName) {
			c.JSON(http.StatusConflict, errResp("duplicate_name"))
			return
		}
		c.JSON(http.StatusInternalServerError, errResp("internal_error"))
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "reason": "", "token": tokenHex})
}

// POST /admin/tokens/delete
func (a *SptAPI) handleAdminDeleteToken(c *gin.Context) {
	if !checkClearance(c, ClearanceAdminModifyDelete) {
		return
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Name == "" {
		c.JSON(http.StatusBadRequest, errResp("bad_request"))
		return
	}

	target, err := a.db.GetAuthToken(body.Name)
	if err != nil {
		// Not found — respond identically to clearance failure.
		c.JSON(http.StatusForbidden, errResp("bad_auth"))
		return
	}

	// Cannot delete a token with clearance >= your own.
	if target.Clearance >= clearanceFromCtx(c) {
		c.JSON(http.StatusForbidden, errResp("bad_auth"))
		return
	}

	if err := a.db.DeleteAuthToken(a.ctx, body.Name); err != nil {
		c.JSON(http.StatusInternalServerError, errResp("internal_error"))
		return
	}

	c.JSON(http.StatusOK, okResp())
}
