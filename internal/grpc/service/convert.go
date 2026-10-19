package service

import (
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/autofik-development/autofik.dev.api.garages.getway/internal/cms"
	pb "github.com/autofik-development/autofik.dev.api.garages.getway/internal/grpc/pb/garages/v1"
)

func toGarage(g cms.Garage) *pb.Garage {
	return &pb.Garage{
		Id:              g.ID,
		GaragesAdminId:  g.GaragesAdminID,
		GarageName:      g.GarageName,
		AddressLocation: g.AddressLocation,
		Latitude:        g.Latitude,
		Longitude:       g.Longitude,
		ExperienceYears: int32Value(g.ExperienceYears),
		Days:            g.Days,
		OpenTime:        g.OpenTime,
		CloseTime:       g.CloseTime,
		IsVerified:      g.IsVerified,
		IsInsured:       g.IsInsured,
		Status:          g.Status,
		CreatedAt:       g.CreatedAt,
		UpdatedAt:       g.UpdatedAt,
	}
}

func toGarageDetail(g cms.GarageDetail) *pb.GarageDetail {
	services := make([]*pb.GarageService, 0, len(g.Services))
	for _, service := range g.Services {
		services = append(services, toGarageService(service))
	}
	staff := make([]*pb.StaffMember, 0, len(g.Staff))
	for _, member := range g.Staff {
		staff = append(staff, toStaffMember(member))
	}
	return &pb.GarageDetail{
		Garage:   toGarage(g.Garage),
		Services: services,
		Staff:    staff,
	}
}

func toGarageRef(r *cms.GarageRef) *pb.GarageRef {
	if r == nil {
		return nil
	}
	return &pb.GarageRef{
		Id:              r.ID,
		GarageName:      r.GarageName,
		AddressLocation: r.AddressLocation,
		Latitude:        r.Latitude,
		Longitude:       r.Longitude,
	}
}

func toGarageService(s cms.GarageService) *pb.GarageService {
	return &pb.GarageService{
		Id:              s.ID,
		GarageId:        s.GarageID,
		ServiceName:     s.ServiceName,
		Description:     s.Description,
		ServiceCategory: s.ServiceCategory,
		DurationMinutes: int32Value(s.DurationMinutes),
		Price:           s.Price,
		IsAvailable:     s.IsAvailable,
		CreatedAt:       s.CreatedAt,
		UpdatedAt:       s.UpdatedAt,
		Garage:          toGarageRef(s.Garage),
	}
}

func toStaffMember(s cms.Staff) *pb.StaffMember {
	return &pb.StaffMember{
		Id:        s.ID,
		GarageId:  s.GarageID,
		Name:      s.Name,
		Age:       s.Age,
		Gender:    s.Gender,
		Position:  s.Position,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
		Garage:    toGarageRef(s.Garage),
	}
}

// int32Value keeps a nullable CMS Int nullable on the wire: the wrapper
// marshals as a plain number, or as null when unset, where a bare int32
// would turn "unknown" into 0.
func int32Value(value *int32) *wrapperspb.Int32Value {
	if value == nil {
		return nil
	}
	return wrapperspb.Int32(*value)
}
