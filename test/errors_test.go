package test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRESTCMSErrors checks every way the CMS can fail maps onto the right
// HTTP status and envelope. The GraphQL bodies are the shapes @nestjs/apollo
// produces: an HttpException outside 400/401/403/422 comes back as
// INTERNAL_SERVER_ERROR with its real status in extensions.originalError, so
// these cases are what keep "Garage not found" from becoming a 500.
func TestRESTCMSErrors(t *testing.T) {
	cases := []struct {
		name       string
		cmsStatus  int
		cmsBody    string
		wantStatus int
		wantBody   string
	}{
		{
			name:      "not found keeps its status and message",
			cmsStatus: http.StatusOK,
			cmsBody: `{"errors": [{"message": "Garage not found", "path": ["result"],
				"extensions": {"code": "INTERNAL_SERVER_ERROR", "status": 404,
					"originalError": {"message": "Garage not found", "error": "Not Found", "statusCode": 404}}}],
				"data": null}`,
			wantStatus: http.StatusNotFound,
			wantBody:   `{"status": "error", "code": "NOT_FOUND", "message": "Garage not found"}`,
		},
		{
			name:      "conflict keeps its status and message",
			cmsStatus: http.StatusOK,
			cmsBody: `{"errors": [{"message": "Garage already offers a MAINTENANCE service", "path": ["result"],
				"extensions": {"code": "INTERNAL_SERVER_ERROR", "status": 409,
					"originalError": {"message": "Garage already offers a MAINTENANCE service", "error": "Conflict", "statusCode": 409}}}],
				"data": null}`,
			wantStatus: http.StatusConflict,
			wantBody:   `{"status": "error", "code": "ALREADY_EXISTS", "message": "Garage already offers a MAINTENANCE service"}`,
		},
		{
			name:      "validation pipe reasons replace the generic message",
			cmsStatus: http.StatusOK,
			cmsBody: `{"errors": [{"message": "Bad Request Exception", "path": ["result"],
				"extensions": {"code": "BAD_REQUEST",
					"originalError": {"message": ["garageName must be longer than or equal to 2 characters",
						"latitude must be a latitude string or number"], "error": "Bad Request", "statusCode": 400}}}],
				"data": null}`,
			wantStatus: http.StatusBadRequest,
			wantBody: `{"status": "error", "code": "INVALID_ARGUMENT",
				"message": "garageName must be longer than or equal to 2 characters, latitude must be a latitude string or number"}`,
		},
		{
			name:      "business rule rejection",
			cmsStatus: http.StatusOK,
			cmsBody: `{"errors": [{"message": "closeTime must be later than openTime", "path": ["result"],
				"extensions": {"code": "BAD_REQUEST",
					"originalError": {"message": "closeTime must be later than openTime", "error": "Bad Request", "statusCode": 400}}}],
				"data": null}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"status": "error", "code": "INVALID_ARGUMENT", "message": "closeTime must be later than openTime"}`,
		},
		{
			name:      "variable coercion failure is the caller's",
			cmsStatus: http.StatusBadRequest,
			cmsBody: `{"errors": [{"message": "Variable \"$input\" got invalid value \"OPEN\" at \"input.status\"; Value \"OPEN\" does not exist in \"GarageStatus\" enum.",
				"extensions": {"code": "BAD_USER_INPUT"}}]}`,
			wantStatus: http.StatusBadRequest,
			wantBody: `{"status": "error", "code": "INVALID_ARGUMENT",
				"message": "Variable \"$input\" got invalid value \"OPEN\" at \"input.status\"; Value \"OPEN\" does not exist in \"GarageStatus\" enum."}`,
		},
		{
			name:      "unexpected cms failure hides its message",
			cmsStatus: http.StatusOK,
			cmsBody: `{"errors": [{"message": "Internal server error", "path": ["result"],
				"extensions": {"code": "INTERNAL_SERVER_ERROR"}}], "data": null}`,
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"status": "error", "code": "INTERNAL_ERROR", "message": "garages cms request failed"}`,
		},
		{
			name:      "5xx http exception hides its message",
			cmsStatus: http.StatusOK,
			cmsBody: `{"errors": [{"message": "connect ECONNREFUSED 10.0.0.5:5432", "path": ["result"],
				"extensions": {"code": "INTERNAL_SERVER_ERROR", "status": 503,
					"originalError": {"message": "connect ECONNREFUSED 10.0.0.5:5432", "statusCode": 503}}}],
				"data": null}`,
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `{"status": "error", "code": "SERVICE_UNAVAILABLE", "message": "garages cms request failed with status 503"}`,
		},
		{
			name:      "schema drift is the gateway's fault, not the caller's",
			cmsStatus: http.StatusBadRequest,
			cmsBody: `{"errors": [{"message": "Cannot query field \"garageNmae\" on type \"Garage\".",
				"extensions": {"code": "GRAPHQL_VALIDATION_FAILED"}}]}`,
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"status": "error", "code": "INTERNAL_ERROR", "message": "garages cms request failed"}`,
		},
		{
			name:       "proxy error page",
			cmsStatus:  http.StatusBadGateway,
			cmsBody:    `<html>502 Bad Gateway</html>`,
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `{"status": "error", "code": "SERVICE_UNAVAILABLE", "message": "garages cms request failed with status 502"}`,
		},
		{
			// A wrong GARAGE_CMS_SERVICE_URL path must not read as "garage
			// not found".
			name:       "misrouted request",
			cmsStatus:  http.StatusNotFound,
			cmsBody:    `<html>Cannot POST /graphq</html>`,
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"status": "error", "code": "INTERNAL_ERROR", "message": "garages cms request failed with status 404"}`,
		},
		{
			name:       "success with unreadable body",
			cmsStatus:  http.StatusOK,
			cmsBody:    `<html>not json</html>`,
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"status": "error", "code": "INTERNAL_ERROR", "message": "garages cms returned an unreadable response"}`,
		},
		{
			name:       "success without data",
			cmsStatus:  http.StatusOK,
			cmsBody:    `{"data": null}`,
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"status": "error", "code": "INTERNAL_ERROR", "message": "garages cms returned an unreadable response"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cms, _ := fakeCMS(t, tc.cmsStatus, tc.cmsBody)
			gw := newTestGatewayWithCMS(t, cms.URL+"/graphql")

			status, body := restJSON(t, http.MethodGet, gw.rest.URL+"/garages/v1/garages/"+garageID, "")
			if status != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", status, tc.wantStatus, body)
			}
			assertJSONEqual(t, body, tc.wantBody)
		})
	}
}

func TestRESTCMSUnreachable(t *testing.T) {
	cms := httptest.NewServer(http.NotFoundHandler())
	cmsURL := cms.URL + "/graphql"
	cms.Close()
	gw := newTestGatewayWithCMS(t, cmsURL)

	status, body := restJSON(t, http.MethodGet, gw.rest.URL+"/garages/v1/garages", "")
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body %s", status, body)
	}
	assertJSONEqual(t, body, `{"status": "error", "code": "SERVICE_UNAVAILABLE", "message": "garages cms is unreachable"}`)
}

// TestRESTForwardedHeaders checks the request id reaches the CMS for log
// correlation, and that caller-supplied identity headers do not: the CMS
// reads x-mysabay-user-id without verifying it, so forwarding it would let
// any caller choose who they are. The bearer and service-code are for SSO
// alone.
func TestRESTForwardedHeaders(t *testing.T) {
	cms, calls := fakeCMS(t, http.StatusOK, cmsResult(`[]`))
	gw := newTestGatewayWithCMS(t, cms.URL+"/graphql")

	status, body := restJSONWithHeader(t, http.MethodGet, gw.rest.URL+"/garages/v1/garages", "", http.Header{
		"X-Request-Id":      {"req-123"},
		"X-Mysabay-User-Id": {"42"},
		"X-Signature":       {"forged"},
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", status, body)
	}

	header := receivedCMSCall(t, calls, "Garages").header
	if got := header.Get("X-Request-Id"); got != "req-123" {
		t.Errorf("CMS X-Request-Id = %q, want req-123", got)
	}
	for _, name := range []string{"X-Mysabay-User-Id", "X-Signature", "Authorization", "Service-Code"} {
		if got := header.Get(name); got != "" {
			t.Errorf("CMS received %s = %q, want it not forwarded", name, got)
		}
	}
}
