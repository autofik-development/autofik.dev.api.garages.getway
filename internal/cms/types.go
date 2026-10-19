package cms

// Object types, as the CMS GraphQL schema returns them. A nullable String
// decodes to "" and a nullable Int to nil.

type Garage struct {
	ID              string   `json:"id"`
	GaragesAdminID  string   `json:"garagesAdminId"`
	GarageName      string   `json:"garageName"`
	AddressLocation string   `json:"addressLocation"`
	Latitude        string   `json:"latitude"`
	Longitude       string   `json:"longitude"`
	ExperienceYears *int32   `json:"experienceYears"`
	Days            []string `json:"days"`
	OpenTime        string   `json:"openTime"`
	CloseTime       string   `json:"closeTime"`
	IsVerified      bool     `json:"isVerified"`
	IsInsured       string   `json:"isInsured"`
	Status          string   `json:"status"`
	CreatedAt       string   `json:"createdAt"`
	UpdatedAt       string   `json:"updatedAt"`
}

// GarageDetail is a Garage with its services and staff resolved.
type GarageDetail struct {
	Garage
	Services []GarageService `json:"services"`
	Staff    []Staff         `json:"staff"`
}

type GarageRef struct {
	ID              string `json:"id"`
	GarageName      string `json:"garageName"`
	AddressLocation string `json:"addressLocation"`
	Latitude        string `json:"latitude"`
	Longitude       string `json:"longitude"`
}

type GarageService struct {
	ID              string     `json:"id"`
	GarageID        string     `json:"garageId"`
	ServiceName     string     `json:"serviceName"`
	Description     string     `json:"description"`
	ServiceCategory string     `json:"serviceCategory"`
	DurationMinutes *int32     `json:"durationMinutes"`
	Price           float64    `json:"price"`
	IsAvailable     string     `json:"isAvailable"`
	CreatedAt       string     `json:"createdAt"`
	UpdatedAt       string     `json:"updatedAt"`
	Garage          *GarageRef `json:"garage"`
}

type Staff struct {
	ID        string     `json:"id"`
	GarageID  string     `json:"garageId"`
	Name      string     `json:"name"`
	Age       int32      `json:"age"`
	Gender    string     `json:"gender"`
	Position  string     `json:"position"`
	CreatedAt string     `json:"createdAt"`
	UpdatedAt string     `json:"updatedAt"`
	Garage    *GarageRef `json:"garage"`
}

// Input types. Optional fields are omitted when unset, so the CMS applies
// its own defaults and a partial update leaves the other columns alone.

type GarageFilterInput struct {
	GaragesAdminID string `json:"garagesAdminId,omitempty"`
	Status         string `json:"status,omitempty"`
	IsVerified     *bool  `json:"isVerified,omitempty"`
	IsInsured      string `json:"isInsured,omitempty"`
	Search         string `json:"search,omitempty"`
	Limit          *int32 `json:"limit,omitempty"`
	Offset         *int32 `json:"offset,omitempty"`
}

type CreateGarageInput struct {
	GaragesAdminID  string   `json:"garagesAdminId"`
	GarageName      string   `json:"garageName"`
	AddressLocation string   `json:"addressLocation"`
	Latitude        string   `json:"latitude"`
	Longitude       string   `json:"longitude"`
	ExperienceYears *int32   `json:"experienceYears,omitempty"`
	Days            []string `json:"days,omitempty"`
	OpenTime        string   `json:"openTime,omitempty"`
	CloseTime       string   `json:"closeTime,omitempty"`
	IsInsured       string   `json:"isInsured,omitempty"`
}

type UpdateGarageInput struct {
	ID              string   `json:"id"`
	GarageName      *string  `json:"garageName,omitempty"`
	AddressLocation *string  `json:"addressLocation,omitempty"`
	Latitude        *string  `json:"latitude,omitempty"`
	Longitude       *string  `json:"longitude,omitempty"`
	ExperienceYears *int32   `json:"experienceYears,omitempty"`
	Days            []string `json:"days,omitempty"`
	OpenTime        *string  `json:"openTime,omitempty"`
	CloseTime       *string  `json:"closeTime,omitempty"`
	IsInsured       *string  `json:"isInsured,omitempty"`
}

type SetGarageStatusInput struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type SetGarageVerifiedInput struct {
	ID         string `json:"id"`
	IsVerified bool   `json:"isVerified"`
}

type GarageServiceFilterInput struct {
	GarageID        string `json:"garageId,omitempty"`
	ServiceCategory string `json:"serviceCategory,omitempty"`
	IsAvailable     string `json:"isAvailable,omitempty"`
	Search          string `json:"search,omitempty"`
	Limit           *int32 `json:"limit,omitempty"`
	Offset          *int32 `json:"offset,omitempty"`
}

type CreateGarageServiceInput struct {
	GarageID        string  `json:"garageId"`
	ServiceName     string  `json:"serviceName"`
	Description     string  `json:"description,omitempty"`
	ServiceCategory string  `json:"serviceCategory"`
	DurationMinutes *int32  `json:"durationMinutes,omitempty"`
	Price           float64 `json:"price"`
	IsAvailable     string  `json:"isAvailable,omitempty"`
}

type UpdateGarageServiceInput struct {
	ID              string   `json:"id"`
	ServiceName     *string  `json:"serviceName,omitempty"`
	Description     *string  `json:"description,omitempty"`
	ServiceCategory *string  `json:"serviceCategory,omitempty"`
	DurationMinutes *int32   `json:"durationMinutes,omitempty"`
	Price           *float64 `json:"price,omitempty"`
	IsAvailable     *string  `json:"isAvailable,omitempty"`
}

type SetGarageServiceAvailabilityInput struct {
	ID          string `json:"id"`
	IsAvailable string `json:"isAvailable"`
}

type StaffFilterInput struct {
	GarageID string `json:"garageId,omitempty"`
	Position string `json:"position,omitempty"`
	Gender   string `json:"gender,omitempty"`
	Search   string `json:"search,omitempty"`
	Limit    *int32 `json:"limit,omitempty"`
	Offset   *int32 `json:"offset,omitempty"`
}

type CreateStaffInput struct {
	GarageID string `json:"garageId"`
	Name     string `json:"name"`
	Age      int32  `json:"age"`
	Gender   string `json:"gender"`
	Position string `json:"position"`
}

type UpdateStaffInput struct {
	ID       string  `json:"id"`
	Name     *string `json:"name,omitempty"`
	Age      *int32  `json:"age,omitempty"`
	Gender   *string `json:"gender,omitempty"`
	Position *string `json:"position,omitempty"`
}
