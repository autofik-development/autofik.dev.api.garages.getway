package test

import (
	"context"
	"net/http"
	"testing"

	pb "github.com/autofik-development/autofik.dev.api.garages.getway/internal/grpc/pb/garages/v1"
)

// cmsGarage is a fully populated garage as the CMS returns it; cmsBareGarage
// has every nullable column null.
const (
	cmsGarage = `{"id": "` + garageID + `", "garagesAdminId": "` + adminID + `",
		"garageName": "Autofik Auto Care", "addressLocation": "12 Street 271, Phnom Penh",
		"latitude": "11.5564", "longitude": "104.9282", "experienceYears": 8,
		"days": ["MONDAY", "TUESDAY"], "openTime": "08:00:00", "closeTime": "17:30:00",
		"isVerified": false, "isInsured": "TRUE", "status": "PENDING",
		"createdAt": "2026-10-19T08:00:00.000Z", "updatedAt": "2026-10-19T09:00:00.000Z"}`

	cmsBareGarage = `{"id": "` + garageID + `", "garagesAdminId": "` + adminID + `",
		"garageName": "Bare Garage", "addressLocation": "Street 1",
		"latitude": "11.5", "longitude": "104.9", "experienceYears": null,
		"days": null, "openTime": null, "closeTime": null,
		"isVerified": true, "isInsured": "FALSE", "status": "ACTIVE",
		"createdAt": "2026-10-19T08:00:00.000Z", "updatedAt": "2026-10-19T08:00:00.000Z"}`

	wantGarage = `{"id": "` + garageID + `", "garages_admin_id": "` + adminID + `",
		"garage_name": "Autofik Auto Care", "address_location": "12 Street 271, Phnom Penh",
		"latitude": "11.5564", "longitude": "104.9282", "experience_years": 8,
		"days": ["MONDAY", "TUESDAY"], "open_time": "08:00:00", "close_time": "17:30:00",
		"is_verified": false, "is_insured": "TRUE", "status": "PENDING",
		"created_at": "2026-10-19T08:00:00.000Z", "updated_at": "2026-10-19T09:00:00.000Z"}`

	wantBareGarage = `{"id": "` + garageID + `", "garages_admin_id": "` + adminID + `",
		"garage_name": "Bare Garage", "address_location": "Street 1",
		"latitude": "11.5", "longitude": "104.9", "experience_years": null,
		"days": [], "open_time": "", "close_time": "",
		"is_verified": true, "is_insured": "FALSE", "status": "ACTIVE",
		"created_at": "2026-10-19T08:00:00.000Z", "updated_at": "2026-10-19T08:00:00.000Z"}`
)

// TestRESTListGarages also pins the REST contract's shape: snake_case keys,
// the envelope, and nulls kept as null (integers) or "" (strings) rather
// than dropped.
func TestRESTListGarages(t *testing.T) {
	cms, calls := fakeCMS(t, http.StatusOK, cmsResult(`[`+cmsGarage+`, `+cmsBareGarage+`]`))
	gw := newTestGatewayWithCMS(t, cms.URL+"/graphql")

	status, body := restJSON(t, http.MethodGet,
		gw.rest.URL+"/garages/v1/garages?status=active&is_verified=false&search=auto&limit=10&offset=20", "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", status, body)
	}

	assertJSONEqual(t, body, `{"status": "success", "message": "Garages retrieved successfully",
		"code": "GARAGE_LIST_SUCCESS", "data": {"garages": [`+wantGarage+`, `+wantBareGarage+`]}}`)

	// is_verified=false must reach the CMS as false, not be dropped as the
	// zero value; unset filters must not be sent at all.
	call := receivedCMSCall(t, calls, "Garages")
	assertVariables(t, call, `{"filter": {"status": "ACTIVE", "isVerified": false,
		"search": "auto", "limit": 10, "offset": 20}}`)
}

func TestRESTListGaragesWithoutFilters(t *testing.T) {
	cms, calls := fakeCMS(t, http.StatusOK, cmsResult(`[]`))
	gw := newTestGatewayWithCMS(t, cms.URL+"/graphql")

	status, body := restJSON(t, http.MethodGet, gw.rest.URL+"/garages/v1/garages", "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", status, body)
	}
	assertJSONEqual(t, body, `{"status": "success", "message": "Garages retrieved successfully",
		"code": "GARAGE_LIST_SUCCESS", "data": {"garages": []}}`)

	assertVariables(t, receivedCMSCall(t, calls, "Garages"), `{"filter": {}}`)
}

func TestRESTGetGarage(t *testing.T) {
	cms, calls := fakeCMS(t, http.StatusOK, cmsResult(withFields(cmsGarage,
		`"services": [`+cmsGarageService+`], "staff": [`+cmsStaff+`]`)))
	gw := newTestGatewayWithCMS(t, cms.URL+"/graphql")

	status, body := restJSON(t, http.MethodGet, gw.rest.URL+"/garages/v1/garages/"+garageID, "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", status, body)
	}

	assertJSONEqual(t, body, `{"status": "success", "message": "Garage retrieved successfully",
		"code": "GARAGE_GET_SUCCESS", "data": {"garage": `+wantGarage+`,
		"services": [`+wantGarageService(`null`)+`], "staff": [`+wantStaff(`null`)+`]}}`)

	assertVariables(t, receivedCMSCall(t, calls, "Garage"), `{"id": "`+garageID+`"}`)
}

func TestRESTCreateGarage(t *testing.T) {
	cms, calls := fakeCMS(t, http.StatusOK, cmsResult(cmsGarage))
	gw := newTestGatewayWithCMS(t, cms.URL+"/graphql")

	status, body := restJSON(t, http.MethodPost, gw.rest.URL+"/garages/v1/garages", `{
		"garages_admin_id": "`+adminID+`", "garage_name": "Autofik Auto Care",
		"address_location": "12 Street 271, Phnom Penh",
		"latitude": "11.5564", "longitude": "104.9282",
		"experience_years": 8, "days": ["monday", "Tuesday"],
		"open_time": "08:00", "close_time": "17:30"}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", status, body)
	}
	assertJSONEqual(t, body, `{"status": "success", "message": "Garage created successfully",
		"code": "GARAGE_CREATE_SUCCESS", "data": `+wantGarage+`}`)

	// Enum values are upper-cased; is_insured was not sent, so it is left
	// for the CMS to default.
	assertVariables(t, receivedCMSCall(t, calls, "CreateGarage"), `{"input": {
		"garagesAdminId": "`+adminID+`", "garageName": "Autofik Auto Care",
		"addressLocation": "12 Street 271, Phnom Penh",
		"latitude": "11.5564", "longitude": "104.9282",
		"experienceYears": 8, "days": ["MONDAY", "TUESDAY"],
		"openTime": "08:00", "closeTime": "17:30"}}`)
}

// TestRESTUpdateGarageSendsOnlyPresentFields checks PATCH semantics: a field
// the caller sent is forwarded even when it is a zero value, and a field it
// left out is not sent, so the CMS keeps the stored value.
func TestRESTUpdateGarageSendsOnlyPresentFields(t *testing.T) {
	cms, calls := fakeCMS(t, http.StatusOK, cmsResult(cmsGarage))
	gw := newTestGatewayWithCMS(t, cms.URL+"/graphql")

	status, body := restJSON(t, http.MethodPatch, gw.rest.URL+"/garages/v1/garages/"+garageID,
		`{"garage_name": "Renamed", "experience_years": 0, "is_insured": "true"}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", status, body)
	}
	assertJSONEqual(t, body, `{"status": "success", "message": "Garage updated successfully",
		"code": "GARAGE_UPDATE_SUCCESS", "data": `+wantGarage+`}`)

	assertVariables(t, receivedCMSCall(t, calls, "UpdateGarage"), `{"input": {
		"id": "`+garageID+`", "garageName": "Renamed", "experienceYears": 0, "isInsured": "TRUE"}}`)
}

func TestRESTSetGarageStatus(t *testing.T) {
	cms, calls := fakeCMS(t, http.StatusOK, cmsResult(cmsGarage))
	gw := newTestGatewayWithCMS(t, cms.URL+"/graphql")

	status, body := restJSON(t, http.MethodPatch, gw.rest.URL+"/garages/v1/garages/"+garageID+"/status",
		`{"status": "active"}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", status, body)
	}
	assertJSONEqual(t, body, `{"status": "success", "message": "Garage status updated successfully",
		"code": "GARAGE_STATUS_UPDATE_SUCCESS", "data": `+wantGarage+`}`)

	assertVariables(t, receivedCMSCall(t, calls, "SetGarageStatus"),
		`{"input": {"id": "`+garageID+`", "status": "ACTIVE"}}`)
}

func TestRESTSetGarageVerified(t *testing.T) {
	cms, calls := fakeCMS(t, http.StatusOK, cmsResult(cmsGarage))
	gw := newTestGatewayWithCMS(t, cms.URL+"/graphql")

	// false is a real value here, so it must be forwarded.
	status, body := restJSON(t, http.MethodPatch, gw.rest.URL+"/garages/v1/garages/"+garageID+"/verified",
		`{"is_verified": false}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", status, body)
	}
	assertJSONEqual(t, body, `{"status": "success", "message": "Garage verification updated successfully",
		"code": "GARAGE_VERIFIED_UPDATE_SUCCESS", "data": `+wantGarage+`}`)

	assertVariables(t, receivedCMSCall(t, calls, "SetGarageVerified"),
		`{"input": {"id": "`+garageID+`", "isVerified": false}}`)
}

func TestRESTDeleteGarage(t *testing.T) {
	cms, calls := fakeCMS(t, http.StatusOK, cmsResult(`true`))
	gw := newTestGatewayWithCMS(t, cms.URL+"/graphql")

	status, body := restJSON(t, http.MethodDelete, gw.rest.URL+"/garages/v1/garages/"+garageID, "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", status, body)
	}
	assertJSONEqual(t, body, `{"status": "success", "message": "Garage deleted successfully",
		"code": "GARAGE_DELETE_SUCCESS", "data": {"id": "`+garageID+`", "deleted": true}}`)

	assertVariables(t, receivedCMSCall(t, calls, "DeleteGarage"), `{"id": "`+garageID+`"}`)
}

// TestRESTGarageRequestsRejectedByGateway covers the checks the gateway
// makes itself, so the CMS is never called.
func TestRESTGarageRequestsRejectedByGateway(t *testing.T) {
	cases := []struct {
		name, method, path, body, wantMessage string
	}{
		{"non-uuid id", http.MethodGet, "/garages/v1/garages/not-a-uuid", "", "id must be a UUID"},
		{"non-uuid id on delete", http.MethodDelete, "/garages/v1/garages/1", "", "id must be a UUID"},
		{"status missing", http.MethodPatch, "/garages/v1/garages/" + garageID + "/status", `{}`, "status is required"},
		{"is_verified missing", http.MethodPatch, "/garages/v1/garages/" + garageID + "/verified", `{}`, "is_verified is required"},
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

// TestGRPCListGarages calls the gRPC surface directly, without grpc-gateway.
func TestGRPCListGarages(t *testing.T) {
	cms, calls := fakeCMS(t, http.StatusOK, cmsResult(`[`+cmsBareGarage+`]`))
	gw := newTestGatewayWithCMS(t, cms.URL+"/graphql")

	limit := int32(5)
	resp, err := pb.NewGaragesGatewayServiceClient(gw.conn).ListGarages(context.Background(),
		&pb.ListGaragesRequest{GaragesAdminId: adminID, Limit: &limit})
	if err != nil {
		t.Fatalf("ListGarages: %v", err)
	}

	garages := resp.GetData().GetGarages()
	if len(garages) != 1 {
		t.Fatalf("got %d garages, want 1", len(garages))
	}
	if got := garages[0]; got.GetId() != garageID || got.GetExperienceYears() != nil || !got.GetIsVerified() {
		t.Errorf("garage = %v, want id %s, null experience_years, verified", got, garageID)
	}

	assertVariables(t, receivedCMSCall(t, calls, "Garages"),
		`{"filter": {"garagesAdminId": "`+adminID+`", "limit": 5}}`)
}
