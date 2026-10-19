package service

import (
	"context"
	"regexp"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/autofik-development/autofik.dev.api.garages.getway/internal/cms"
	pb "github.com/autofik-development/autofik.dev.api.garages.getway/internal/grpc/pb/garages/v1"
)

// ServiceName identifies this gateway in the HealthCheck payload.
const ServiceName = "garages-gateway"

// statusSuccess is the envelope status on every successful response; the
// error envelope is written by internal/gateway.
const statusSuccess = "success"

type GaragesGatewayServer struct {
	pb.UnimplementedGaragesGatewayServiceServer
	cms *cms.Client
}

func NewGaragesGatewayServer(cmsClient *cms.Client) *GaragesGatewayServer {
	return &GaragesGatewayServer{cms: cmsClient}
}

func (s *GaragesGatewayServer) HealthCheck(ctx context.Context, req *pb.HealthCheckRequest) (*pb.HealthCheckResponse, error) {
	return &pb.HealthCheckResponse{
		Status:  "ok",
		Service: ServiceName,
	}, nil
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// requireID validates an id taken from the URL path. The CMS's by-id queries
// and deletes take a bare `ID!` with no validation, so a malformed id would
// reach Postgres and come back as a 500; checking it here makes it a 400.
func requireID(field, value string) (string, error) {
	id := strings.TrimSpace(value)
	if id == "" {
		return "", status.Errorf(codes.InvalidArgument, "%s is required", field)
	}
	if !uuidPattern.MatchString(id) {
		return "", status.Errorf(codes.InvalidArgument, "%s must be a UUID", field)
	}
	return id, nil
}

// requireValue rejects an empty required field before it costs a CMS round
// trip and comes back as a GraphQL coercion error.
func requireValue(field, value string) (string, error) {
	value = enumValue(value)
	if value == "" {
		return "", status.Errorf(codes.InvalidArgument, "%s is required", field)
	}
	return value, nil
}

// enumValue normalises an enum-like field. Every CMS enum value is
// upper-case, so "active" is accepted as "ACTIVE".
func enumValue(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func enumPtr(value *string) *string {
	if value == nil {
		return nil
	}
	normalised := enumValue(*value)
	return &normalised
}

func enumList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	normalised := make([]string, len(values))
	for i, value := range values {
		normalised[i] = enumValue(value)
	}
	return normalised
}

func deleteResponse(id string, deleted bool, message, code string) *pb.DeleteResponse {
	return &pb.DeleteResponse{
		Status:  statusSuccess,
		Message: message,
		Code:    code,
		Data:    &pb.DeleteResult{Id: id, Deleted: deleted},
	}
}
