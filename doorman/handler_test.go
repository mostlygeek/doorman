package doorman

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type AllowedResponse struct {
	Allowed    bool
	Principals Principals
}

type ReloadResponse struct {
	Success bool
	Message string
}

type ErrorResponse struct {
	Message string
}

func performRequest(r http.Handler, method, path string, body io.Reader) *httptest.ResponseRecorder {
	req, _ := http.NewRequest(method, path, body)
	req.Header.Set("Origin", "https://sample.yaml")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func performAllowed(t *testing.T, r *gin.Engine, body io.Reader, expected int, response interface{}) {
	w := performRequest(r, "POST", "/allowed", body)
	require.Equal(t, expected, w.Code)
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.Nil(t, err)
}

func TestAllowedGet(t *testing.T) {
	r := gin.New()
	doorman := sampleDoorman()
	SetupRoutes(r, doorman)

	w := performRequest(r, "GET", "/allowed", nil)
	assert.Equal(t, w.Code, http.StatusNotFound)
}

func TestAllowedVerifiesJWT(t *testing.T) {
	// Create config with jwtIssuer
	tmpfile, _ := ioutil.TempFile("", "")
	defer os.Remove(tmpfile.Name()) // clean up
	tmpfile.Write([]byte(`
service: https://sample.yaml
jwtIssuer: https://auth.mozilla.auth0.com/
policies:
  -
    id: "1"
    action: update
`))

	doorman := New([]string{tmpfile.Name()})
	// Will initialize JWT validator (ie. download public keys)
	doorman.LoadPolicies()

	r := gin.New()
	SetupRoutes(r, doorman)

	authzRequest := Request{}
	token, _ := json.Marshal(authzRequest)
	body := bytes.NewBuffer(token)
	var response ErrorResponse
	// Missing Authorization header.
	performAllowed(t, r, body, http.StatusUnauthorized, &response)
	assert.Equal(t, "Token not found", response.Message)

	// Other routes do not require JWT.
	w := performRequest(r, "POST", "/__reload__", nil)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestAllowedHandlerBadRequest(t *testing.T) {
	var errResp ErrorResponse

	// Empty body
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	c.Request, _ = http.NewRequest("POST", "/allowed", nil)
	allowedHandler(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	json.Unmarshal(w.Body.Bytes(), &errResp)
	assert.Equal(t, errResp.Message, "Missing body")

	// Invalid JSON
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)

	body := bytes.NewBuffer([]byte("{\"random\\;mess\"}"))
	c.Request, _ = http.NewRequest("POST", "/allowed", body)
	allowedHandler(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	json.Unmarshal(w.Body.Bytes(), &errResp)
	assert.Contains(t, errResp.Message, "invalid character ';'")

	// Missing principals when JWT middleware not enabled.
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)

	body = bytes.NewBuffer([]byte("{\"action\":\"update\"}"))
	c.Request, _ = http.NewRequest("POST", "/allowed", body)
	allowedHandler(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	json.Unmarshal(w.Body.Bytes(), &errResp)
	assert.Contains(t, errResp.Message, "missing principals")

	doorman := sampleDoorman()

	// Posted principals with JWT middleware enabled.
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Set(DoormanContextKey, doorman)
	c.Set(PrincipalsContextKey, Principals{"userid:maria"}) // Simulate JWT middleware.
	authzRequest := Request{
		Principals: Principals{"userid:superuser"},
	}
	post, _ := json.Marshal(authzRequest)
	body = bytes.NewBuffer(post)
	c.Request, _ = http.NewRequest("POST", "/allowed", body)
	allowedHandler(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	json.Unmarshal(w.Body.Bytes(), &errResp)
	assert.Contains(t, errResp.Message, "cannot submit principals with JWT enabled")
}

func TestAllowedHandler(t *testing.T) {
	var resp AllowedResponse

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	doorman := sampleDoorman()
	c.Set(DoormanContextKey, doorman)

	// Using principals from context (JWT middleware)
	c.Set(PrincipalsContextKey, Principals{"userid:maria"})

	authzRequest := Request{
		Action: "update",
	}
	post, _ := json.Marshal(authzRequest)
	body := bytes.NewBuffer(post)
	c.Request, _ = http.NewRequest("POST", "/allowed", body)
	c.Request.Header.Set("Origin", "https://sample.yaml")

	allowedHandler(c)

	assert.Equal(t, http.StatusOK, w.Code)
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.True(t, resp.Allowed)
	assert.Equal(t, Principals{"userid:maria", "tag:admins"}, resp.Principals)
}

func TestAllowedHandlerRoles(t *testing.T) {
	var resp AllowedResponse

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	doorman := sampleDoorman()
	c.Set(DoormanContextKey, doorman)

	// Expand principals from context roles
	authzRequest := Request{
		Principals: Principals{"userid:bob"},
		Action:     "update",
		Resource:   "pto",
		Context: Context{
			"roles": []string{"editor"},
		},
	}
	post, _ := json.Marshal(authzRequest)
	body := bytes.NewBuffer(post)
	c.Request, _ = http.NewRequest("POST", "/allowed", body)

	allowedHandler(c)

	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, Principals{"userid:bob", "role:editor"}, resp.Principals)
}

func TestReloadHandler(t *testing.T) {
	tmpfile, _ := ioutil.TempFile("", "")
	defer os.Remove(tmpfile.Name()) // clean up

	tmpfile.Write([]byte(`
service: a
policies:
  -
    id: "1"
    action: update
`))

	var resp ReloadResponse

	doorman := New([]string{tmpfile.Name()})

	// Reload same file.
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(DoormanContextKey, doorman)
	c.Request, _ = http.NewRequest("POST", "/__reload__", nil)

	reloadHandler(c)

	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.True(t, resp.Success)

	// Reload bad file.
	tmpfile.Write([]byte("*some$bad@cont\tent"))
	w.Flush()

	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Set(DoormanContextKey, doorman)
	c.Request, _ = http.NewRequest("POST", "/__reload__", nil)

	reloadHandler(c)

	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "did not find expected alphabetic or numeric character")
}
