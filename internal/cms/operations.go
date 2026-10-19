package cms

import "context"

// Selection sets shared by the documents below. The list queries leave out
// the Garage.services/staff and GarageService.garage/Staff.garage resolve
// fields on purpose: each one costs the CMS another database query per row.
const (
	garageFields        = "id garagesAdminId garageName addressLocation latitude longitude experienceYears days openTime closeTime isVerified isInsured status createdAt updatedAt"
	garageRefFields     = "id garageName addressLocation latitude longitude"
	garageServiceFields = "id garageId serviceName description serviceCategory durationMinutes price isAvailable createdAt updatedAt"
	staffFields         = "id garageId name age gender position createdAt updatedAt"
)

// Operation is one fixed GraphQL document. Every document aliases its root
// field as `result`, so a single response type decodes them all.
type Operation struct {
	Name     string
	Document string
}

var (
	opGarages = Operation{"Garages",
		`query Garages($filter: GarageFilterInput) { result: garages(filter: $filter) { ` + garageFields + ` } }`}
	opGarage = Operation{"Garage",
		`query Garage($id: ID!) { result: garage(id: $id) { ` + garageFields +
			` services { ` + garageServiceFields + ` } staff { ` + staffFields + ` } } }`}
	opCreateGarage = Operation{"CreateGarage",
		`mutation CreateGarage($input: CreateGarageInput!) { result: createGarage(input: $input) { ` + garageFields + ` } }`}
	opUpdateGarage = Operation{"UpdateGarage",
		`mutation UpdateGarage($input: UpdateGarageInput!) { result: updateGarage(input: $input) { ` + garageFields + ` } }`}
	opSetGarageStatus = Operation{"SetGarageStatus",
		`mutation SetGarageStatus($input: SetGarageStatusInput!) { result: setGarageStatus(input: $input) { ` + garageFields + ` } }`}
	opSetGarageVerified = Operation{"SetGarageVerified",
		`mutation SetGarageVerified($input: SetGarageVerifiedInput!) { result: setGarageVerified(input: $input) { ` + garageFields + ` } }`}
	opDeleteGarage = Operation{"DeleteGarage",
		`mutation DeleteGarage($id: ID!) { result: deleteGarage(id: $id) }`}

	opGarageServices = Operation{"GarageServices",
		`query GarageServices($filter: GarageServiceFilterInput) { result: garageServices(filter: $filter) { ` + garageServiceFields + ` } }`}
	opGarageServicesCount = Operation{"GarageServicesCount",
		`query GarageServicesCount($filter: GarageServiceFilterInput) { result: garageServicesCount(filter: $filter) }`}
	opGarageService = Operation{"GarageService",
		`query GarageService($id: ID!) { result: garageService(id: $id) { ` + garageServiceFields + ` garage { ` + garageRefFields + ` } } }`}
	opCreateGarageService = Operation{"CreateGarageService",
		`mutation CreateGarageService($input: CreateGarageServiceInput!) { result: createGarageService(input: $input) { ` + garageServiceFields + ` garage { ` + garageRefFields + ` } } }`}
	opUpdateGarageService = Operation{"UpdateGarageService",
		`mutation UpdateGarageService($input: UpdateGarageServiceInput!) { result: updateGarageService(input: $input) { ` + garageServiceFields + ` garage { ` + garageRefFields + ` } } }`}
	opSetGarageServiceAvailability = Operation{"SetGarageServiceAvailability",
		`mutation SetGarageServiceAvailability($input: SetGarageServiceAvailabilityInput!) { result: setGarageServiceAvailability(input: $input) { ` + garageServiceFields + ` garage { ` + garageRefFields + ` } } }`}
	opDeleteGarageService = Operation{"DeleteGarageService",
		`mutation DeleteGarageService($id: ID!) { result: deleteGarageService(id: $id) }`}

	opStaffMembers = Operation{"StaffMembers",
		`query StaffMembers($filter: StaffFilterInput) { result: staffMembers(filter: $filter) { ` + staffFields + ` } }`}
	opStaffMember = Operation{"StaffMember",
		`query StaffMember($id: ID!) { result: staffMember(id: $id) { ` + staffFields + ` garage { ` + garageRefFields + ` } } }`}
	opCreateStaff = Operation{"CreateStaff",
		`mutation CreateStaff($input: CreateStaffInput!) { result: createStaff(input: $input) { ` + staffFields + ` garage { ` + garageRefFields + ` } } }`}
	opUpdateStaff = Operation{"UpdateStaff",
		`mutation UpdateStaff($input: UpdateStaffInput!) { result: updateStaff(input: $input) { ` + staffFields + ` garage { ` + garageRefFields + ` } } }`}
	opDeleteStaff = Operation{"DeleteStaff",
		`mutation DeleteStaff($id: ID!) { result: deleteStaff(id: $id) }`}
)

// Operations returns every document the gateway sends, so a test can
// validate them against the CMS schema.
func Operations() []Operation {
	return []Operation{
		opGarages, opGarage, opCreateGarage, opUpdateGarage, opSetGarageStatus, opSetGarageVerified, opDeleteGarage,
		opGarageServices, opGarageServicesCount, opGarageService, opCreateGarageService, opUpdateGarageService,
		opSetGarageServiceAvailability, opDeleteGarageService,
		opStaffMembers, opStaffMember, opCreateStaff, opUpdateStaff, opDeleteStaff,
	}
}

// ------------------------------------------------------------------ garages

func (c *Client) Garages(ctx context.Context, filter GarageFilterInput) ([]Garage, error) {
	return do[[]Garage](ctx, c, opGarages, map[string]any{"filter": filter})
}

func (c *Client) Garage(ctx context.Context, id string) (GarageDetail, error) {
	return do[GarageDetail](ctx, c, opGarage, map[string]any{"id": id})
}

func (c *Client) CreateGarage(ctx context.Context, input CreateGarageInput) (Garage, error) {
	return do[Garage](ctx, c, opCreateGarage, map[string]any{"input": input})
}

func (c *Client) UpdateGarage(ctx context.Context, input UpdateGarageInput) (Garage, error) {
	return do[Garage](ctx, c, opUpdateGarage, map[string]any{"input": input})
}

func (c *Client) SetGarageStatus(ctx context.Context, input SetGarageStatusInput) (Garage, error) {
	return do[Garage](ctx, c, opSetGarageStatus, map[string]any{"input": input})
}

func (c *Client) SetGarageVerified(ctx context.Context, input SetGarageVerifiedInput) (Garage, error) {
	return do[Garage](ctx, c, opSetGarageVerified, map[string]any{"input": input})
}

func (c *Client) DeleteGarage(ctx context.Context, id string) (bool, error) {
	return do[bool](ctx, c, opDeleteGarage, map[string]any{"id": id})
}

// ---------------------------------------------------------- garage services

func (c *Client) GarageServices(ctx context.Context, filter GarageServiceFilterInput) ([]GarageService, error) {
	return do[[]GarageService](ctx, c, opGarageServices, map[string]any{"filter": filter})
}

func (c *Client) GarageServicesCount(ctx context.Context, filter GarageServiceFilterInput) (int32, error) {
	return do[int32](ctx, c, opGarageServicesCount, map[string]any{"filter": filter})
}

func (c *Client) GarageService(ctx context.Context, id string) (GarageService, error) {
	return do[GarageService](ctx, c, opGarageService, map[string]any{"id": id})
}

func (c *Client) CreateGarageService(ctx context.Context, input CreateGarageServiceInput) (GarageService, error) {
	return do[GarageService](ctx, c, opCreateGarageService, map[string]any{"input": input})
}

func (c *Client) UpdateGarageService(ctx context.Context, input UpdateGarageServiceInput) (GarageService, error) {
	return do[GarageService](ctx, c, opUpdateGarageService, map[string]any{"input": input})
}

func (c *Client) SetGarageServiceAvailability(ctx context.Context, input SetGarageServiceAvailabilityInput) (GarageService, error) {
	return do[GarageService](ctx, c, opSetGarageServiceAvailability, map[string]any{"input": input})
}

func (c *Client) DeleteGarageService(ctx context.Context, id string) (bool, error) {
	return do[bool](ctx, c, opDeleteGarageService, map[string]any{"id": id})
}

// -------------------------------------------------------------------- staff

func (c *Client) StaffMembers(ctx context.Context, filter StaffFilterInput) ([]Staff, error) {
	return do[[]Staff](ctx, c, opStaffMembers, map[string]any{"filter": filter})
}

func (c *Client) StaffMember(ctx context.Context, id string) (Staff, error) {
	return do[Staff](ctx, c, opStaffMember, map[string]any{"id": id})
}

func (c *Client) CreateStaff(ctx context.Context, input CreateStaffInput) (Staff, error) {
	return do[Staff](ctx, c, opCreateStaff, map[string]any{"input": input})
}

func (c *Client) UpdateStaff(ctx context.Context, input UpdateStaffInput) (Staff, error) {
	return do[Staff](ctx, c, opUpdateStaff, map[string]any{"input": input})
}

func (c *Client) DeleteStaff(ctx context.Context, id string) (bool, error) {
	return do[bool](ctx, c, opDeleteStaff, map[string]any{"id": id})
}
