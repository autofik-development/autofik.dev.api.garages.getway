package test

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/validator"

	"github.com/autofik-development/autofik.dev.api.garages.getway/internal/cms"
)

// TestOperationsMatchCMSSchema validates every GraphQL document the gateway
// sends against testdata/cms.graphql, a snapshot of the CMS schema, and runs
// the variables its Go input types encode to through the same schema. That
// catches a misspelt field in a document or a json tag the CMS does not
// know, neither of which the fake-CMS tests can see. Refresh the snapshot
// with scripts/print-cms-schema.ts when the CMS schema changes.
func TestOperationsMatchCMSSchema(t *testing.T) {
	sdl, err := os.ReadFile("testdata/cms.graphql")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	schema, err := gqlparser.LoadSchema(&ast.Source{Name: "cms.graphql", Input: string(sdl)})
	if err != nil {
		t.Fatalf("load schema: %v", err)
	}

	samples := sampleVariables()

	for _, op := range cms.Operations() {
		t.Run(op.Name, func(t *testing.T) {
			doc, errs := gqlparser.LoadQuery(schema, op.Document)
			if len(errs) > 0 {
				t.Fatalf("document does not match the CMS schema: %v", errs)
			}
			if len(doc.Operations) != 1 || doc.Operations[0].Name != op.Name {
				t.Fatalf("document must hold exactly one operation named %q", op.Name)
			}

			variables, ok := samples[op.Name]
			if !ok {
				t.Fatalf("no sample variables for %s; add them to sampleVariables", op.Name)
			}
			for name, value := range variables {
				assertEveryFieldSet(t, name, value)
			}

			// Round-trip through JSON so the validator sees exactly what
			// goes over the wire.
			encoded, err := json.Marshal(variables)
			if err != nil {
				t.Fatalf("encode variables: %v", err)
			}
			var decoded map[string]any
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatalf("decode variables: %v", err)
			}
			if _, err := validator.VariableValues(schema, doc.Operations[0], decoded); err != nil {
				t.Errorf("variables %s rejected by the CMS schema: %v", encoded, err)
			}
		})
	}
}

// assertEveryFieldSet makes sure a sample input sets every field, so each
// json tag is actually checked - including fields added later.
func assertEveryFieldSet(t *testing.T, name string, value any) {
	t.Helper()
	v := reflect.ValueOf(value)
	if v.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).IsZero() {
			t.Errorf("sample %s leaves %s unset; set it so its json tag is validated", name, v.Type().Field(i).Name)
		}
	}
}

func sampleVariables() map[string]map[string]any {
	i32 := func(v int32) *int32 { return &v }
	str := func(v string) *string { return &v }
	yes := true
	price := 45.5

	garageFilter := cms.GarageFilterInput{
		GaragesAdminID: adminID, Status: "ACTIVE", IsVerified: &yes, IsInsured: "TRUE",
		Search: "auto", Limit: i32(10), Offset: i32(0),
	}
	serviceFilter := cms.GarageServiceFilterInput{
		GarageID: garageID, ServiceCategory: "MAINTENANCE", IsAvailable: "TRUE",
		Search: "oil", Limit: i32(10), Offset: i32(0),
	}
	staffFilter := cms.StaffFilterInput{
		GarageID: garageID, Position: "MANAGER", Gender: "FEMALE",
		Search: "dara", Limit: i32(10), Offset: i32(0),
	}

	return map[string]map[string]any{
		"Garages": {"filter": garageFilter},
		"Garage":  {"id": garageID},
		"CreateGarage": {"input": cms.CreateGarageInput{
			GaragesAdminID: adminID, GarageName: "Autofik Auto Care", AddressLocation: "Phnom Penh",
			Latitude: "11.5564", Longitude: "104.9282", ExperienceYears: i32(8),
			Days: []string{"MONDAY"}, OpenTime: "08:00", CloseTime: "17:30", IsInsured: "TRUE",
		}},
		"UpdateGarage": {"input": cms.UpdateGarageInput{
			ID: garageID, GarageName: str("Renamed"), AddressLocation: str("Phnom Penh"),
			Latitude: str("11.5564"), Longitude: str("104.9282"), ExperienceYears: i32(9),
			Days: []string{"SUNDAY"}, OpenTime: str("09:00"), CloseTime: str("18:00"), IsInsured: str("FALSE"),
		}},
		"SetGarageStatus":   {"input": cms.SetGarageStatusInput{ID: garageID, Status: "SUSPENDED"}},
		"SetGarageVerified": {"input": cms.SetGarageVerifiedInput{ID: garageID, IsVerified: true}},
		"DeleteGarage":      {"id": garageID},

		"GarageServices":      {"filter": serviceFilter},
		"GarageServicesCount": {"filter": serviceFilter},
		"GarageService":       {"id": serviceID},
		"CreateGarageService": {"input": cms.CreateGarageServiceInput{
			GarageID: garageID, ServiceName: "Oil Change", Description: "Full synthetic",
			ServiceCategory: "MAINTENANCE", DurationMinutes: i32(30), Price: 45.5, IsAvailable: "TRUE",
		}},
		"UpdateGarageService": {"input": cms.UpdateGarageServiceInput{
			ID: serviceID, ServiceName: str("Oil Change Pro"), Description: str("4L"),
			ServiceCategory: str("ENGINE"), DurationMinutes: i32(45), Price: &price, IsAvailable: str("FALSE"),
		}},
		"SetGarageServiceAvailability": {"input": cms.SetGarageServiceAvailabilityInput{ID: serviceID, IsAvailable: "FALSE"}},
		"DeleteGarageService":          {"id": serviceID},

		"StaffMembers": {"filter": staffFilter},
		"StaffMember":  {"id": staffID},
		"CreateStaff": {"input": cms.CreateStaffInput{
			GarageID: garageID, Name: "Sok Dara", Age: 28, Gender: "MALE", Position: "MANAGER",
		}},
		"UpdateStaff": {"input": cms.UpdateStaffInput{
			ID: staffID, Name: str("Sok Dara"), Age: i32(29), Gender: str("MALE"), Position: str("HR"),
		}},
		"DeleteStaff": {"id": staffID},
	}
}
