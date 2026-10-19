package service

import (
	"context"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/autofik-development/autofik.dev.api.garages.getway/internal/cms"
	pb "github.com/autofik-development/autofik.dev.api.garages.getway/internal/grpc/pb/garages/v1"
)

func (s *GaragesGatewayServer) ListGarageServices(ctx context.Context, req *pb.ListGarageServicesRequest) (*pb.ListGarageServicesResponse, error) {
	result, err := s.cms.GarageServices(ctx, cms.GarageServiceFilterInput{
		GarageID:        strings.TrimSpace(req.GetGarageId()),
		ServiceCategory: enumValue(req.GetServiceCategory()),
		IsAvailable:     enumValue(req.GetIsAvailable()),
		Search:          strings.TrimSpace(req.GetSearch()),
		Limit:           req.Limit,
		Offset:          req.Offset,
	})
	if err != nil {
		return nil, err
	}

	services := make([]*pb.GarageService, 0, len(result))
	for _, service := range result {
		services = append(services, toGarageService(service))
	}

	return &pb.ListGarageServicesResponse{
		Status:  statusSuccess,
		Message: "Garage services retrieved successfully",
		Code:    "GARAGE_SERVICE_LIST_SUCCESS",
		Data:    &pb.ListGarageServicesData{GarageServices: services},
	}, nil
}

func (s *GaragesGatewayServer) CountGarageServices(ctx context.Context, req *pb.CountGarageServicesRequest) (*pb.CountGarageServicesResponse, error) {
	count, err := s.cms.GarageServicesCount(ctx, cms.GarageServiceFilterInput{
		GarageID:        strings.TrimSpace(req.GetGarageId()),
		ServiceCategory: enumValue(req.GetServiceCategory()),
		IsAvailable:     enumValue(req.GetIsAvailable()),
		Search:          strings.TrimSpace(req.GetSearch()),
	})
	if err != nil {
		return nil, err
	}

	return &pb.CountGarageServicesResponse{
		Status:  statusSuccess,
		Message: "Garage services counted successfully",
		Code:    "GARAGE_SERVICE_COUNT_SUCCESS",
		Data:    &pb.CountGarageServicesData{Count: count},
	}, nil
}

func (s *GaragesGatewayServer) GetGarageService(ctx context.Context, req *pb.GetGarageServiceRequest) (*pb.GarageServiceResponse, error) {
	id, err := requireID("id", req.GetId())
	if err != nil {
		return nil, err
	}

	result, err := s.cms.GarageService(ctx, id)
	if err != nil {
		return nil, err
	}

	return garageServiceResponse(result, "Garage service retrieved successfully", "GARAGE_SERVICE_GET_SUCCESS"), nil
}

func (s *GaragesGatewayServer) CreateGarageService(ctx context.Context, req *pb.CreateGarageServiceRequest) (*pb.GarageServiceResponse, error) {
	// Absent must not mean 0: a missing price would otherwise list the
	// service as free.
	if req.Price == nil {
		return nil, status.Error(codes.InvalidArgument, "price is required")
	}

	result, err := s.cms.CreateGarageService(ctx, cms.CreateGarageServiceInput{
		GarageID:        strings.TrimSpace(req.GetGarageId()),
		ServiceName:     req.GetServiceName(),
		Description:     req.GetDescription(),
		ServiceCategory: enumValue(req.GetServiceCategory()),
		DurationMinutes: req.DurationMinutes,
		Price:           req.GetPrice(),
		IsAvailable:     enumValue(req.GetIsAvailable()),
	})
	if err != nil {
		return nil, err
	}

	return garageServiceResponse(result, "Garage service created successfully", "GARAGE_SERVICE_CREATE_SUCCESS"), nil
}

func (s *GaragesGatewayServer) UpdateGarageService(ctx context.Context, req *pb.UpdateGarageServiceRequest) (*pb.GarageServiceResponse, error) {
	id, err := requireID("id", req.GetId())
	if err != nil {
		return nil, err
	}

	result, err := s.cms.UpdateGarageService(ctx, cms.UpdateGarageServiceInput{
		ID:              id,
		ServiceName:     req.ServiceName,
		Description:     req.Description,
		ServiceCategory: enumPtr(req.ServiceCategory),
		DurationMinutes: req.DurationMinutes,
		Price:           req.Price,
		IsAvailable:     enumPtr(req.IsAvailable),
	})
	if err != nil {
		return nil, err
	}

	return garageServiceResponse(result, "Garage service updated successfully", "GARAGE_SERVICE_UPDATE_SUCCESS"), nil
}

func (s *GaragesGatewayServer) SetGarageServiceAvailability(ctx context.Context, req *pb.SetGarageServiceAvailabilityRequest) (*pb.GarageServiceResponse, error) {
	id, err := requireID("id", req.GetId())
	if err != nil {
		return nil, err
	}
	isAvailable, err := requireValue("is_available", req.GetIsAvailable())
	if err != nil {
		return nil, err
	}

	result, err := s.cms.SetGarageServiceAvailability(ctx, cms.SetGarageServiceAvailabilityInput{ID: id, IsAvailable: isAvailable})
	if err != nil {
		return nil, err
	}

	return garageServiceResponse(result, "Garage service availability updated successfully", "GARAGE_SERVICE_AVAILABILITY_UPDATE_SUCCESS"), nil
}

func (s *GaragesGatewayServer) DeleteGarageService(ctx context.Context, req *pb.DeleteGarageServiceRequest) (*pb.DeleteResponse, error) {
	id, err := requireID("id", req.GetId())
	if err != nil {
		return nil, err
	}

	deleted, err := s.cms.DeleteGarageService(ctx, id)
	if err != nil {
		return nil, err
	}

	return deleteResponse(id, deleted, "Garage service deleted successfully", "GARAGE_SERVICE_DELETE_SUCCESS"), nil
}

func garageServiceResponse(service cms.GarageService, message, code string) *pb.GarageServiceResponse {
	return &pb.GarageServiceResponse{
		Status:  statusSuccess,
		Message: message,
		Code:    code,
		Data:    toGarageService(service),
	}
}
