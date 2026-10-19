package test

import (
	"net/http"
	"strings"
	"testing"
)

const (
	cmsGarageRef = `{"id": "` + garageID + `", "garageName": "Autofik Auto Care",
		"addressLocation": "12 Street 271, Phnom Penh", "latitude": "11.5564", "longitude": "104.9282"}`

	wantGarageRef = `{"id": "` + garageID + `", "garage_name": "Autofik Auto Care",
		"address_location": "12 Street 271, Phnom Penh", "latitude": "11.5564", "longitude": "104.9282"}`

	// cmsGarageService is a garage service as list queries return it,
	// without its garage.
	cmsGarageService = `{"id": "` + serviceID + `", "garageId": "` + garageID + `",
		"serviceName": "Oil Change", "description": null, "serviceCategory": "MAINTENANCE",
		"durationMinutes": 30, "price": 45.5, "isAvailable": "TRUE",
		"createdAt": "2026-10-19T08:00:00.000Z", "updatedAt": "2026-10-19T08:00:00.000Z"}`
)

func wantGarageService(garage string) string {
	return `{"id": "` + serviceID + `", "garage_id": "` + garageID + `",
		"service_name": "Oil Change", "description": "", "service_category": "MAINTENANCE",
		"duration_minutes": 30, "price": 45.5, "is_available": "TRUE",
		"created_at": "2026-10-19T08:00:00.000Z", "updated_at": "2026-10-19T08:00:00.000Z",
		"garage": ` + garage + `}`
}

// withFields appends fields to a JSON object literal.
func withFields(object, fields string) string {
	return strings.TrimSuffix(strings.TrimSpace(object), "}") + ", " + fields + "}"
}

func TestRESTListGarageServices(t *testing.T) {
	cms, calls := fakeCMS(t, http.StatusOK, cmsResult(`[`+cmsGarageService+`]`))
	gw := newTestGatewayWithCMS(t, cms.URL+"/graphql")

	status, body := restJSON(t, http.MethodGet,
		gw.rest.URL+"/garages/v1/garage-services?garage_id="+garageID+"&service_category=maintenance&is_available=TRUE", "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", status, body)
	}
	assertJSONEqual(t, body, `{"status": "success", "message": "Garage services retrieved successfully",
		"code": "GARAGE_SERVICE_LIST_SUCCESS", "data": {"garage_services": [`+wantGarageService(`null`)+`]}}`)

	assertVariables(t, receivedCMSCall(t, calls, "GarageServices"),
		`{"filter": {"garageId": "`+garageID+`", "serviceCategory": "MAINTENANCE", "isAvailable": "TRUE"}}`)
}

// TestRESTCountGarageServices also proves route precedence: /count must not
// be captured by the /{id} pattern beside it, which would answer 400 "id
// must be a UUID".
func TestRESTCountGarageServices(t *testing.T) {
	cms, calls := fakeCMS(t, http.StatusOK, cmsResult(`3`))
	gw := newTestGatewayWithCMS(t, cms.URL+"/graphql")

	status, body := restJSON(t, http.MethodGet, gw.rest.URL+"/garages/v1/garage-services/count?garage_id="+garageID, "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", status, body)
	}
	assertJSONEqual(t, body, `{"status": "success", "message": "Garage services counted successfully",
		"code": "GARAGE_SERVICE_COUNT_SUCCESS", "data": {"count": 3}}`)

	assertVariables(t, receivedCMSCall(t, calls, "GarageServicesCount"), `{"filter": {"garageId": "`+garageID+`"}}`)
}

func TestRESTGetGarageService(t *testing.T) {
	cms, calls := fakeCMS(t, http.StatusOK, cmsResult(withFields(cmsGarageService, `"garage": `+cmsGarageRef)))
	gw := newTestGatewayWithCMS(t, cms.URL+"/graphql")

	status, body := restJSON(t, http.MethodGet, gw.rest.URL+"/garages/v1/garage-services/"+serviceID, "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", status, body)
	}
	assertJSONEqual(t, body, `{"status": "success", "message": "Garage service retrieved successfully",
		"code": "GARAGE_SERVICE_GET_SUCCESS", "data": `+wantGarageService(wantGarageRef)+`}`)

	assertVariables(t, receivedCMSCall(t, calls, "GarageService"), `{"id": "`+serviceID+`"}`)
}

func TestRESTCreateGarageService(t *testing.T) {
	cms, calls := fakeCMS(t, http.StatusOK, cmsResult(withFields(cmsGarageService, `"garage": `+cmsGarageRef)))
	gw := newTestGatewayWithCMS(t, cms.URL+"/graphql")

	// A price of 0 is a real price, so it must be forwarded.
	status, body := restJSON(t, http.MethodPost, gw.rest.URL+"/garages/v1/garage-services", `{
		"garage_id": "`+garageID+`", "service_name": "Oil Change",
		"service_category": "maintenance", "duration_minutes": 30, "price": 0}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", status, body)
	}
	assertJSONEqual(t, body, `{"status": "success", "message": "Garage service created successfully",
		"code": "GARAGE_SERVICE_CREATE_SUCCESS", "data": `+wantGarageService(wantGarageRef)+`}`)

	assertVariables(t, receivedCMSCall(t, calls, "CreateGarageService"), `{"input": {
		"garageId": "`+garageID+`", "serviceName": "Oil Change",
		"serviceCategory": "MAINTENANCE", "durationMinutes": 30, "price": 0}}`)
}

func TestRESTUpdateGarageService(t *testing.T) {
	cms, calls := fakeCMS(t, http.StatusOK, cmsResult(withFields(cmsGarageService, `"garage": `+cmsGarageRef)))
	gw := newTestGatewayWithCMS(t, cms.URL+"/graphql")

	status, body := restJSON(t, http.MethodPatch, gw.rest.URL+"/garages/v1/garage-services/"+serviceID,
		`{"price": 60, "description": ""}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", status, body)
	}
	assertJSONEqual(t, body, `{"status": "success", "message": "Garage service updated successfully",
		"code": "GARAGE_SERVICE_UPDATE_SUCCESS", "data": `+wantGarageService(wantGarageRef)+`}`)

	assertVariables(t, receivedCMSCall(t, calls, "UpdateGarageService"),
		`{"input": {"id": "`+serviceID+`", "price": 60, "description": ""}}`)
}

func TestRESTSetGarageServiceAvailability(t *testing.T) {
	cms, calls := fakeCMS(t, http.StatusOK, cmsResult(withFields(cmsGarageService, `"garage": `+cmsGarageRef)))
	gw := newTestGatewayWithCMS(t, cms.URL+"/graphql")

	status, body := restJSON(t, http.MethodPatch, gw.rest.URL+"/garages/v1/garage-services/"+serviceID+"/availability",
		`{"is_available": "false"}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", status, body)
	}
	assertJSONEqual(t, body, `{"status": "success", "message": "Garage service availability updated successfully",
		"code": "GARAGE_SERVICE_AVAILABILITY_UPDATE_SUCCESS", "data": `+wantGarageService(wantGarageRef)+`}`)

	assertVariables(t, receivedCMSCall(t, calls, "SetGarageServiceAvailability"),
		`{"input": {"id": "`+serviceID+`", "isAvailable": "FALSE"}}`)
}

func TestRESTDeleteGarageService(t *testing.T) {
	cms, calls := fakeCMS(t, http.StatusOK, cmsResult(`true`))
	gw := newTestGatewayWithCMS(t, cms.URL+"/graphql")

	status, body := restJSON(t, http.MethodDelete, gw.rest.URL+"/garages/v1/garage-services/"+serviceID, "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", status, body)
	}
	assertJSONEqual(t, body, `{"status": "success", "message": "Garage service deleted successfully",
		"code": "GARAGE_SERVICE_DELETE_SUCCESS", "data": {"id": "`+serviceID+`", "deleted": true}}`)

	assertVariables(t, receivedCMSCall(t, calls, "DeleteGarageService"), `{"id": "`+serviceID+`"}`)
}

func TestRESTGarageServiceRequestsRejectedByGateway(t *testing.T) {
	cases := []struct {
		name, method, path, body, wantMessage string
	}{
		{"price missing", http.MethodPost, "/garages/v1/garage-services",
			`{"garage_id": "` + garageID + `", "service_name": "Oil Change", "service_category": "MAINTENANCE"}`,
			"price is required"},
		{"is_available missing", http.MethodPatch, "/garages/v1/garage-services/" + serviceID + "/availability", `{}`,
			"is_available is required"},
		{"non-uuid id", http.MethodGet, "/garages/v1/garage-services/oil-change", "", "id must be a UUID"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cms, calls := fakeCMS(t, http.StatusOK, `{}`)
			gw := newTestGatewayWithCMS(t, cms.URL+"/graphql")

			status, body := restJSON(t, tc.method, gw.rest.URL+tc.path, tc.body)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body %s", status, body)
			}
			assertJSONEqual(t, body, `{"status": "error", "code": "INVALID_ARGUMENT", "message": "`+tc.wantMessage+`"}`)
			assertNoCMSCall(t, calls)
		})
	}
}
