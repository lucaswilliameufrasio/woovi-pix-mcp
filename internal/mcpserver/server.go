package mcpserver

import (
	"context"
	"errors"

	"github.com/lucaseufrasio/woovi-pix-mcp/internal/charge"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetChargeInput struct {
	ID string `json:"id" jsonschema:"Woovi charge identifier or correlation ID"`
}

type CreateChargeInput struct {
	Reference        string `json:"reference" jsonschema:"Stable business reference and idempotency key for this charge"`
	AmountCents      int64  `json:"amount_cents" jsonschema:"Charge value in whole BRL cents"`
	ExpiresInSeconds int64  `json:"expires_in_seconds" jsonschema:"Charge lifetime in seconds, from 300 to 2592000"`
}

type Server struct {
	client       charge.Client
	creator      charge.ChargeCreator
	store        charge.OperationRepository
	tenant       string
	writeEnabled bool
}

func New(client charge.Client) (*Server, error) {
	if client == nil {
		return nil, errors.New("charge client is required")
	}
	return &Server{client: client}, nil
}

func NewWithWrites(client charge.Client, creator charge.ChargeCreator, store charge.OperationRepository, tenant string, enabled bool) (*Server, error) {
	server, err := New(client)
	if err != nil {
		return nil, err
	}
	if enabled && (creator == nil || store == nil || tenant == "") {
		return nil, errors.New("writes require a creator, operation store and configured account")
	}
	server.creator, server.store, server.tenant, server.writeEnabled = creator, store, tenant, enabled
	return server, nil
}

func (s *Server) MCP() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "woovi-pix-mcp", Version: "0.1.0"}, nil)
	openWorld := true
	mcp.AddTool(server, &mcp.Tool{
		Name: "pix_get_charge", Description: "Consulta uma cobrança Pix Woovi pelo identificador ou correlation ID.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &openWorld},
	}, s.getCharge)
	if s.writeEnabled {
		mcp.AddTool(server, &mcp.Tool{
			Name: "pix_create_charge", Description: "Cria uma cobrança Pix Woovi com valor limitado. A referência é idempotente; resultado incerto não deve ser repetido automaticamente.",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPointer(false), OpenWorldHint: &openWorld},
		}, s.createCharge)
	}
	return server
}

func boolPointer(value bool) *bool { return &value }

func (s *Server) getCharge(ctx context.Context, _ *mcp.CallToolRequest, input GetChargeInput) (*mcp.CallToolResult, charge.Charge, error) {
	result, err := s.client.GetCharge(ctx, input.ID)
	if err != nil {
		return nil, charge.Charge{}, err
	}
	return nil, result, nil
}

func (s *Server) createCharge(ctx context.Context, _ *mcp.CallToolRequest, input CreateChargeInput) (*mcp.CallToolResult, map[string]any, error) {
	if !s.writeEnabled || s.creator == nil || s.store == nil {
		return nil, nil, errors.New("charge creation is disabled")
	}
	request := charge.CreateChargeRequest{CorrelationID: input.Reference, AmountCents: input.AmountCents, ExpiresInSeconds: input.ExpiresInSeconds}
	if err := charge.ValidateCreateCharge(request); err != nil {
		return nil, nil, err
	}
	hash, err := charge.RequestHash(request)
	if err != nil {
		return nil, nil, errors.New("unable to fingerprint charge request")
	}
	operation, created, err := s.store.Reserve(ctx, s.tenant, "pix_create_charge", request.CorrelationID, hash)
	if err != nil {
		_ = s.store.Audit(ctx, s.tenant, "", "pix_create_charge", "REJECTED")
		return nil, nil, err
	}
	if !created {
		if operation.Status == charge.OperationCompleted {
			_ = s.store.Audit(ctx, s.tenant, operation.ID, "pix_create_charge", "REPLAYED")
			return nil, map[string]any{"charge": operation.Charge, "replayed": true}, nil
		}
		if operation.Status == charge.OperationUnknown {
			reconciled, lookupErr := s.client.GetCharge(ctx, request.CorrelationID)
			if lookupErr != nil {
				_ = s.store.Audit(ctx, s.tenant, operation.ID, "pix_create_charge", "UNKNOWN")
				return nil, map[string]any{"operation_id": operation.ID, "status": charge.OperationUnknown, "retry_safe": false, "message": "Charge remains unresolved; reconcile by reference before retrying."}, nil
			}
			if reconciled.Reference != request.CorrelationID || reconciled.AmountCents != request.AmountCents {
				_ = s.store.Audit(ctx, s.tenant, operation.ID, "pix_create_charge", "RECONCILE_MISMATCH")
				return nil, nil, errors.New("provider charge does not match the pending request")
			}
			if reconcileErr := s.store.Reconcile(ctx, operation.ID, reconciled); reconcileErr != nil {
				_ = s.store.Audit(ctx, s.tenant, operation.ID, "pix_create_charge", "RECONCILE_PERSIST_FAILED")
				return nil, map[string]any{"operation_id": operation.ID, "status": charge.OperationUnknown, "retry_safe": false, "message": "Charge found but reconciliation was not persisted."}, nil
			}
			_ = s.store.Audit(ctx, s.tenant, operation.ID, "pix_create_charge", "RECONCILED")
			return nil, map[string]any{"charge": reconciled, "replayed": true, "reconciled": true}, nil
		}
		return nil, map[string]any{"operation_id": operation.ID, "status": operation.Status, "retry_safe": false, "message": "A previous attempt is unresolved. Reconcile the charge by reference before any retry."}, nil
	}
	if err := s.store.Audit(ctx, s.tenant, operation.ID, "pix_create_charge", "STARTED"); err != nil {
		return nil, nil, errors.New("audit persistence failed; provider request was not made")
	}
	result, err := s.creator.CreateCharge(ctx, request)
	if err != nil {
		if markErr := s.store.MarkUnknown(ctx, operation.ID); markErr != nil {
			return nil, nil, errors.New("charge outcome unknown and operation persistence failed; reconcile by reference")
		}
		reconciled, lookupErr := s.client.GetCharge(ctx, request.CorrelationID)
		if lookupErr == nil {
			if reconciled.Reference != request.CorrelationID || reconciled.AmountCents != request.AmountCents {
				_ = s.store.Audit(ctx, s.tenant, operation.ID, "pix_create_charge", "RECONCILE_MISMATCH")
				return nil, nil, errors.New("provider charge does not match the pending request; operation remains unresolved")
			}
			if reconcileErr := s.store.Reconcile(ctx, operation.ID, reconciled); reconcileErr == nil {
				_ = s.store.Audit(ctx, s.tenant, operation.ID, "pix_create_charge", "RECONCILED")
				return nil, map[string]any{"charge": reconciled, "replayed": false, "reconciled": true}, nil
			}
		}
		_ = s.store.Audit(ctx, s.tenant, operation.ID, "pix_create_charge", "UNKNOWN")
		return nil, map[string]any{"operation_id": operation.ID, "status": charge.OperationUnknown, "retry_safe": false, "message": "Charge outcome unknown; reconcile by reference before retrying."}, nil
	}
	if result.Reference != request.CorrelationID || result.AmountCents != request.AmountCents {
		_ = s.store.MarkUnknown(ctx, operation.ID)
		_ = s.store.Audit(ctx, s.tenant, operation.ID, "pix_create_charge", "PROVIDER_RESULT_MISMATCH")
		return nil, nil, errors.New("provider response does not match the requested charge; operation requires reconciliation")
	}
	if err := s.store.Complete(ctx, operation.ID, result); err != nil {
		_ = s.store.Audit(ctx, s.tenant, operation.ID, "pix_create_charge", "COMPLETE_PERSIST_FAILED")
		return nil, map[string]any{"operation_id": operation.ID, "status": charge.OperationUnknown, "retry_safe": false, "message": "Provider created the charge but local completion failed; reconcile by reference."}, nil
	}
	if err := s.store.Audit(ctx, s.tenant, operation.ID, "pix_create_charge", "COMPLETED"); err != nil {
		return nil, map[string]any{"operation_id": operation.ID, "status": charge.OperationCompleted, "retry_safe": true, "message": "Charge was persisted, but audit confirmation failed."}, nil
	}
	return nil, map[string]any{"charge": result, "replayed": false}, nil
}
