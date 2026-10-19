package service

import (
	"context"
	"strings"

	"github.com/autofik-development/autofik.dev.api.garages.getway/internal/cms"
	pb "github.com/autofik-development/autofik.dev.api.garages.getway/internal/grpc/pb/garages/v1"
)

func (s *GaragesGatewayServer) ListStaffMembers(ctx context.Context, req *pb.ListStaffMembersRequest) (*pb.ListStaffMembersResponse, error) {
	result, err := s.cms.StaffMembers(ctx, cms.StaffFilterInput{
		GarageID: strings.TrimSpace(req.GetGarageId()),
		Position: enumValue(req.GetPosition()),
		Gender:   enumValue(req.GetGender()),
		Search:   strings.TrimSpace(req.GetSearch()),
		Limit:    req.Limit,
		Offset:   req.Offset,
	})
	if err != nil {
		return nil, err
	}

	staff := make([]*pb.StaffMember, 0, len(result))
	for _, member := range result {
		staff = append(staff, toStaffMember(member))
	}

	return &pb.ListStaffMembersResponse{
		Status:  statusSuccess,
		Message: "Staff members retrieved successfully",
		Code:    "STAFF_LIST_SUCCESS",
		Data:    &pb.ListStaffMembersData{StaffMembers: staff},
	}, nil
}

func (s *GaragesGatewayServer) GetStaffMember(ctx context.Context, req *pb.GetStaffMemberRequest) (*pb.StaffMemberResponse, error) {
	id, err := requireID("id", req.GetId())
	if err != nil {
		return nil, err
	}

	result, err := s.cms.StaffMember(ctx, id)
	if err != nil {
		return nil, err
	}

	return staffMemberResponse(result, "Staff member retrieved successfully", "STAFF_GET_SUCCESS"), nil
}

func (s *GaragesGatewayServer) CreateStaffMember(ctx context.Context, req *pb.CreateStaffMemberRequest) (*pb.StaffMemberResponse, error) {
	result, err := s.cms.CreateStaff(ctx, cms.CreateStaffInput{
		GarageID: strings.TrimSpace(req.GetGarageId()),
		Name:     req.GetName(),
		Age:      req.GetAge(),
		Gender:   enumValue(req.GetGender()),
		Position: enumValue(req.GetPosition()),
	})
	if err != nil {
		return nil, err
	}

	return staffMemberResponse(result, "Staff member created successfully", "STAFF_CREATE_SUCCESS"), nil
}

func (s *GaragesGatewayServer) UpdateStaffMember(ctx context.Context, req *pb.UpdateStaffMemberRequest) (*pb.StaffMemberResponse, error) {
	id, err := requireID("id", req.GetId())
	if err != nil {
		return nil, err
	}

	result, err := s.cms.UpdateStaff(ctx, cms.UpdateStaffInput{
		ID:       id,
		Name:     req.Name,
		Age:      req.Age,
		Gender:   enumPtr(req.Gender),
		Position: enumPtr(req.Position),
	})
	if err != nil {
		return nil, err
	}

	return staffMemberResponse(result, "Staff member updated successfully", "STAFF_UPDATE_SUCCESS"), nil
}

func (s *GaragesGatewayServer) DeleteStaffMember(ctx context.Context, req *pb.DeleteStaffMemberRequest) (*pb.DeleteResponse, error) {
	id, err := requireID("id", req.GetId())
	if err != nil {
		return nil, err
	}

	deleted, err := s.cms.DeleteStaff(ctx, id)
	if err != nil {
		return nil, err
	}

	return deleteResponse(id, deleted, "Staff member deleted successfully", "STAFF_DELETE_SUCCESS"), nil
}

func staffMemberResponse(member cms.Staff, message, code string) *pb.StaffMemberResponse {
	return &pb.StaffMemberResponse{
		Status:  statusSuccess,
		Message: message,
		Code:    code,
		Data:    toStaffMember(member),
	}
}
