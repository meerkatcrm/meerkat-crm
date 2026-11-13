package controllers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"meerkat/config"
	"meerkat/models"
	"meerkat/services"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ssoOnlyConfig is an instance with OIDC on and DISABLE_PASSWORD_LOGIN set.
func ssoOnlyConfig() *config.Config {
	cfg := &config.Config{JWTSecretKey: "mysecretkey", JWTExpiryHours: 24, FrontendURL: "http://localhost:3000"}
	cfg.OIDC.Enabled = true
	cfg.OIDC.ProviderURL = "https://sso.example.com/realm"
	cfg.OIDC.ProviderName = "Pocket ID"
	cfg.OIDC.ClientSecret = "must-stay-private"
	cfg.PasswordLoginDisabled = true
	return cfg
}

func TestAuthConfigHandler(t *testing.T) {
	get := func(cfg *config.Config) map[string]any {
		router := gin.New()
		router.GET("/auth/config", AuthConfigHandler(cfg))
		w := doRequest(router, http.MethodGet, "/auth/config", nil, "")
		require.Equal(t, http.StatusOK, w.Code)
		assert.NotContains(t, w.Body.String(), "must-stay-private")
		return decodeJSON[map[string]any](t, w)
	}

	assert.Equal(t, map[string]any{
		"oidc_enabled": false, "password_login_enabled": true, "registration_enabled": true,
	}, get(&config.Config{}))

	assert.Equal(t, map[string]any{
		"oidc_enabled": false, "password_login_enabled": true, "registration_enabled": false,
	}, get(&config.Config{RegistrationDisabled: true}))

	// DISABLE_PASSWORD_LOGIN without OIDC is ignored: nobody could sign in otherwise.
	assert.Equal(t, map[string]any{
		"oidc_enabled": false, "password_login_enabled": true, "registration_enabled": true,
	}, get(&config.Config{PasswordLoginDisabled: true}))

	withOIDC := ssoOnlyConfig()
	withOIDC.PasswordLoginDisabled = false
	assert.Equal(t, map[string]any{
		"oidc_enabled": true, "oidc_provider_name": "Pocket ID", "password_login_enabled": true, "registration_enabled": true,
	}, get(withOIDC))

	// SSO-only: no password sign-in, and so no password registration either.
	assert.Equal(t, map[string]any{
		"oidc_enabled": true, "oidc_provider_name": "Pocket ID", "password_login_enabled": false, "registration_enabled": false,
	}, get(ssoOnlyConfig()))
}

func assertPasswordLoginDisabled(t *testing.T, code int, body []byte) {
	t.Helper()
	assert.Equal(t, http.StatusForbidden, code)
	assert.Contains(t, string(body), "password_login_disabled")
}

func TestSSOOnly_RefusesPasswordLogin(t *testing.T) {
	cfg := ssoOnlyConfig()
	db, router := setupRouter()
	router.POST("/login", func(c *gin.Context) { LoginUser(c, cfg) })

	hash, _ := services.HashPassword(strongPassword)
	require.NoError(t, db.Create(&models.User{Username: "pw", Email: "pw@example.com", Password: hash}).Error)

	body, _ := json.Marshal(map[string]string{"identifier": "pw", "password": strongPassword})
	w := doRequest(router, http.MethodPost, "/login", bytes.NewReader(body), "application/json")
	assertPasswordLoginDisabled(t, w.Code, w.Body.Bytes())
	assert.Empty(t, w.Result().Cookies(), "no session may be issued")
}

func TestSSOOnly_RefusesPasswordReset(t *testing.T) {
	cfg := ssoOnlyConfig()
	db, router := setupRouter()

	hash, _ := services.HashPassword(strongPassword)
	token, tokenHash, _ := services.GeneratePasswordResetToken()
	expires := services.PasswordResetExpiry()
	user := models.User{Username: "pw", Email: "pw@example.com", Password: hash, PasswordResetTokenHash: &tokenHash, PasswordResetExpiresAt: &expires}
	require.NoError(t, db.Create(&user).Error)

	router.POST("/password-reset/request", func(c *gin.Context) {
		c.Set("validated", &models.PasswordResetRequestInput{Email: "pw@example.com"})
		RequestPasswordReset(c, cfg)
	})
	router.POST("/password-reset/confirm", func(c *gin.Context) {
		c.Set("cfg", *cfg)
		c.Set("validated", &models.PasswordResetConfirmInput{Token: token, Password: strongPasswordAlt})
		ConfirmPasswordReset(c)
	})

	w := doRequest(router, http.MethodPost, "/password-reset/request", nil, "")
	assertPasswordLoginDisabled(t, w.Code, w.Body.Bytes())

	w = doRequest(router, http.MethodPost, "/password-reset/confirm", nil, "")
	assertPasswordLoginDisabled(t, w.Code, w.Body.Bytes())

	var stored models.User
	require.NoError(t, db.First(&stored, user.ID).Error)
	assert.Equal(t, hash, stored.Password, "password must be unchanged")
}

func TestSSOOnly_RefusesRegistration(t *testing.T) {
	_, router := setupRouter()
	router.POST("/register", func(c *gin.Context) {
		c.Set("validated", &models.UserRegistrationInput{Username: "new", Email: "new@example.com", Password: strongPassword})
		RegisterUser(ssoOnlyConfig())(c)
	})

	w := doRequest(router, http.MethodPost, "/register", nil, "")
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "registration_disabled")
}
