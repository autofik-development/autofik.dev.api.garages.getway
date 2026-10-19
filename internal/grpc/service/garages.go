package service

import (
	"context"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/autofik-development/autofik.dev.api.garages.getway/internal/cms"
	pb "github.com/autofik-development/autofik.dev.api.garages.getway/internal/grpc/pb/garages/v1"
)

func (s *GaragesGatewayServer) ListGarages(ctx context.Context, req *pb.ListGaragesRequest) (*pb.ListGaragesResponse, error) {
	result, err := s.cms.Garages(ctx, cms.GarageFilterInput{
		GaragesAdminID: strings.TrimSpace(req.GetGaragesAdminId()),
		Status:         enumValue(req.GetStatus()),
		IsVerified:     req.IsVerified,
		IsInsured:      enumValue(req.GetIsInsured()),
		Search:         strings.TrimSpace(req.GetSearch()),
		Limit:          req.Limit,
		Offset:         req.Offset,
	})
	if err != nil {
		return nil, err
	}

	garages := make([]*pb.Garage, 0, len(result))
	for _, garage := range result {
		garages = append(garages, toGarage(garage))
	}

	return &pb.ListGaragesResponse{
		Status:  statusSuccess,
		Message: "Garages retrieved successfully",
		Code:    "GARAGE_LIST_SUCCESS",
		Data:    &pb.ListGaragesData{Garages: garages},
	}, nil
}

func (s *GaragesGatewayServer) GetGarage(ctx context.Context, req *pb.GetGarageRequest) (*pb.GetGarageResponse, error) {
	id, err := requireID("id", req.GetId())
	if err != nil {
		return nil, err
	}

	result, err := s.cms.Garage(ctx, id)
	if err != nil {
		return nil, err
	}

	return &pb.GetGarageResponse{
		Status:  statusSuccess,
		Message: "Garage retrieved successfully",
		Code:    "GARAGE_GET_SUCCESS",
		Data:    toGarageDetail(result),
	}, nil
}

func (s *GaragesGatewayServer) CreateGarage(ctx context.Context, req *pb.CreateGarageRequest) (*pb.GarageResponse, error) {
	result, err := s.cms.CreateGarage(ctx, cms.CreateGarageInput{
		GaragesAdminID:  strings.TrimSpace(req.GetGaragesAdminId()),
		GarageName:      req.GetGarageName(),
		AddressLocation: req.GetAddressLocation(),
		Latitude:        strings.TrimSpace(req.GetLatitude()),
		Longitude:       strings.TrimSpace(req.GetLongitude()),
		ExperienceYears: req.ExperienceYears,
		Days:            enumList(req.GetDays()),
		OpenTime:        strings.TrimSpace(req.GetOpenTime()),
		CloseTime:       strings.TrimSpace(req.GetCloseTime()),
		IsInsured:       enumValue(req.GetIsInsured()),
	})
	if err != nil {
		return nil, err
	}

	return garageResponse(result, "Garage created successfully", "GARAGE_CREATE_SUCCESS"), nil
}

func (s *GaragesGatewayServer) UpdateGarage(ctx context.Context, req *pb.UpdateGarageRequest) (*pb.GarageResponse, error) {
	id, err := requireID("id", req.GetId())
	if err != nil {
		return nil, err
	}

	result, err := s.cms.UpdateGarage(ctx, cms.UpdateGarageInput{
		ID:              id,
		GarageName:      req.GarageName,
		AddressLocation: req.AddressLocation,
		Latitude:        req.Latitude,
		Longitude:       req.Longitude,
		ExperienceYears: req.ExperienceYears,
		Days:            enumList(req.GetDays()),
		OpenTime:        req.OpenTime,
		CloseTime:       req.CloseTime,
		IsInsured:       enumPtr(req.IsInsured),
	})
	if err != nil {
		return nil, err
	}

	return garageResponse(result, "Garage updated successfully", "GARAGE_UPDATE_SUCCESS"), nil
}

func (s *GaragesGatewayServer) SetGarageStatus(ctx context.Context, req *pb.SetGarageStatusRequest) (*pb.GarageResponse, error) {
	id, err := requireID("id", req.GetId())
	if err != nil {
		return nil, err
	}
	garageStatus, err := requireValue("status", req.GetStatus())
	if err != nil {
		return nil, err
	}

	result, err := s.cms.SetGarageStatus(ctx, cms.SetGarageStatusInput{ID: id, Status: garageStatus})
	if err != nil {
		return nil, err
	}

	return garageResponse(result, "Garage status updated successfully", "GARAGE_STATUS_UPDATE_SUCCESS"), nil
}

func (s *GaragesGatewayServer) SetGarageVerified(ctx context.Context, req *pb.SetGarageVerifiedRequest) (*pb.GarageResponse, error) {
	id, err := requireID("id", req.GetId())
	if err != nil {
		return nil, err
	}
	// Absent must not mean false: an empty PATCH body would otherwise
	// silently un-verify the garage.
	if req.IsVerified == nil {
		return nil, status.Error(codes.InvalidArgument, "is_verified is required")
	}

	result, err := s.cms.SetGarageVerified(ctx, cms.SetGarageVerifiedInput{ID: id, IsVerified: req.GetIsVerified()})
	if err != nil {
		return nil, err
	}

	return garageResponse(result, "Garage verification updated successfully", "GARAGE_VERIFIED_UPDATE_SUCCESS"), nil
}

func (s *GaragesGatewayServer) DeleteGarage(ctx context.Context, req *pb.DeleteGarageRequest) (*pb.DeleteResponse, error) {
	id, err := requireID("id", req.GetId())
	if err != nil {
		return nil, err
	}

	deleted, err := s.cms.DeleteGarage(ctx, id)
	if err != nil {
		return nil, err
	}

	return deleteResponse(id, deleted, "Garage deleted successfully", "GARAGE_DELETE_SUCCESS"), nil
}

func garageResponse(garage cms.Garage, message, code string) *pb.GarageResponse {
	return &pb.GarageResponse{
		Status:  statusSuccess,
		Message: message,
		Code:    code,
		Data:    toGarage(garage),
	}
}
