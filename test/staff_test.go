package test

import (
	"net/http"
	"testing"
)

// cmsStaff is a staff member as list queries return them, without their
// garage.
const cmsStaff = `{"id": "` + staffID + `", "garageId": "` + garageID + `",
	"name": "Sok Dara", "age": 28, "gender": "MALE", "position": "MANAGER",
	"createdAt": "2026-10-19T08:00:00.000Z", "updatedAt": "2026-10-19T08:00:00.000Z"}`

func wantStaff(garage string) string {
	return `{"id": "` + staffID + `", "garage_id": "` + garageID + `",
		"name": "Sok Dara", "age": 28, "gender": "MALE", "position": "MANAGER",
		"created_at": "2026-10-19T08:00:00.000Z", "updated_at": "2026-10-19T08:00:00.000Z",
		"garage": ` + garage + `}`
}

func TestRESTListStaffMembers(t *testing.T) {
	cms, calls := fakeCMS(t, http.StatusOK, cmsResult(`[`+cmsStaff+`]`))
	gw := newTestGatewayWithCMS(t, cms.URL+"/graphql")

	status, body := restJSON(t, http.MethodGet,
		gw.rest.URL+"/garages/v1/staff-members?garage_id="+garageID+"&position=manager&search=dara", "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", status, body)
	}
	assertJSONEqual(t, body, `{"status": "success", "message": "Staff members retrieved successfully",
		"code": "STAFF_LIST_SUCCESS", "data": {"staff_members": [`+wantStaff(`null`)+`]}}`)

	assertVariables(t, receivedCMSCall(t, calls, "StaffMembers"),
		`{"filter": {"garageId": "`+garageID+`", "position": "MANAGER", "search": "dara"}}`)
}

func TestRESTGetStaffMember(t *testing.T) {
	cms, calls := fakeCMS(t, http.StatusOK, cmsResult(withFields(cmsStaff, `"garage": `+cmsGarageRef)))
	gw := newTestGatewayWithCMS(t, cms.URL+"/graphql")

	status, body := restJSON(t, http.MethodGet, gw.rest.URL+"/garages/v1/staff-members/"+staffID, "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", status, body)
	}
	assertJSONEqual(t, body, `{"status": "success", "message": "Staff member retrieved successfully",
		"code": "STAFF_GET_SUCCESS", "data": `+wantStaff(wantGarageRef)+`}`)

	assertVariables(t, receivedCMSCall(t, calls, "StaffMember"), `{"id": "`+staffID+`"}`)
}

func TestRESTCreateStaffMember(t *testing.T) {
	cms, calls := fakeCMS(t, http.StatusOK, cmsResult(withFields(cmsStaff, `"garage": `+cmsGarageRef)))
	gw := newTestGatewayWithCMS(t, cms.URL+"/graphql")

	status, body := restJSON(t, http.MethodPost, gw.rest.URL+"/garages/v1/staff-members", `{
		"garage_id": "`+garageID+`", "name": "Sok Dara", "age": 28,
		"gender": "male", "position": "manager"}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", status, body)
	}
	assertJSONEqual(t, body, `{"status": "success", "message": "Staff member created successfully",
		"code": "STAFF_CREATE_SUCCESS", "data": `+wantStaff(wantGarageRef)+`}`)

	assertVariables(t, receivedCMSCall(t, calls, "CreateStaff"), `{"input": {
		"garageId": "`+garageID+`", "name": "Sok Dara", "age": 28,
		"gender": "MALE", "position": "MANAGER"}}`)
}

func TestRESTUpdateStaffMember(t *testing.T) {
	cms, calls := fakeCMS(t, http.StatusOK, cmsResult(withFields(cmsStaff, `"garage": `+cmsGarageRef)))
	gw := newTestGatewayWithCMS(t, cms.URL+"/graphql")

	status, body := restJSON(t, http.MethodPatch, gw.rest.URL+"/garages/v1/staff-members/"+staffID,
		`{"position": "hr"}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", status, body)
	}
	assertJSONEqual(t, body, `{"status": "success", "message": "Staff member updated successfully",
		"code": "STAFF_UPDATE_SUCCESS", "data": `+wantStaff(wantGarageRef)+`}`)

	assertVariables(t, receivedCMSCall(t, calls, "UpdateStaff"),
		`{"input": {"id": "`+staffID+`", "position": "HR"}}`)
}

func TestRESTDeleteStaffMember(t *testing.T) {
	cms, calls := fakeCMS(t, http.StatusOK, cmsResult(`true`))
	gw := newTestGatewayWithCMS(t, cms.URL+"/graphql")

	status, body := restJSON(t, http.MethodDelete, gw.rest.URL+"/garages/v1/staff-members/"+staffID, "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", status, body)
	}
	assertJSONEqual(t, body, `{"status": "success", "message": "Staff member deleted successfully",
		"code": "STAFF_DELETE_SUCCESS", "data": {"id": "`+staffID+`", "deleted": true}}`)

	assertVariables(t, receivedCMSCall(t, calls, "DeleteStaff"), `{"id": "`+staffID+`"}`)
}
