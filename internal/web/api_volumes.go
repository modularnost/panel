package web

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/containerd/errdefs"
	"github.com/danielgtaylor/huma/v2"

	"modularnost/internal/docker"
)

const volumeRoute = "/api/v1/nodes/{node}/volumes"

type volumeNode struct {
	Node string `path:"node" pattern:"^[a-z0-9]+$" maxLength:"64"`
}

type volumePath struct {
	volumeNode
	Name string `path:"name" pattern:"^[a-zA-Z0-9][a-zA-Z0-9_.-]*$" maxLength:"63"`
}

type serviceVolumeInput struct {
	ID   string `path:"id"`
	Body struct {
		Node     string `json:"node" pattern:"^[a-z0-9]+$" maxLength:"64"`
		Name     string `json:"name" pattern:"^[a-zA-Z0-9][a-zA-Z0-9_.-]*$" maxLength:"63"`
		Target   string `json:"target" maxLength:"4096"`
		ReadOnly bool   `json:"read_only,omitempty"`
	}
}

type volumeOutput struct{ Body docker.Volume }
type volumeListOutput struct {
	Body []docker.Volume `nullable:"false"`
}

func volumeProblem(err error) error {
	switch {
	case errdefs.IsUnavailable(err):
		return problem(http.StatusServiceUnavailable, "node_unavailable", "the selected volume node is unavailable")
	case errdefs.IsNotFound(err):
		return problem(http.StatusNotFound, "not_found", "volume or service not found")
	case errdefs.IsConflict(err), errdefs.IsFailedPrecondition(err), errors.Is(err, docker.ErrNetworkConflict):
		return problem(http.StatusConflict, "volume_conflict", "the volume is in use or the service placement or mount conflicts")
	case errdefs.IsInvalidArgument(err), errors.Is(err, docker.ErrInvalidNetwork):
		return problem(http.StatusUnprocessableEntity, "invalid_input", "use a local volume and an absolute container path on a standalone service")
	default:
		return backendProblem(http.StatusBadGateway, "docker_error", err)
	}
}

func (s *Server) checkVolumeNode(ctx context.Context, id string) error {
	nodes, err := s.Docker.Nodes(ctx)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(nodes, func(node docker.Node) bool { return node.ID == id && node.Healthy() }) {
		return errdefs.ErrUnavailable
	}
	return nil
}

func (s *Server) nodeVolume(ctx context.Context, node, name string, create bool) (docker.Volume, error) {
	var volume docker.Volume
	method := http.MethodGet
	op := s.Docker.LocalVolume
	if create {
		method, op = http.MethodPut, s.Docker.CreateLocalVolume
	}
	err := s.withLocalNode(ctx, node, func() error {
		var err error
		volume, err = op(ctx, name)
		return err
	}, method, "/volumes/"+name, nil, &volume)
	volume.Node = node
	return volume, err
}

func setVolumeAttachments(v *docker.Volume, node string, attachments map[string][]string) {
	v.Node = node
	v.Services = append([]string{}, attachments[v.Name]...)
}

func moduleVolumeName(key string) string {
	name := "modularnost-module-" + key
	if docker.ValidName(name) {
		return name
	}
	return fmt.Sprintf("modularnost-module-%x", sha256.Sum224([]byte(key)))[:63]
}

func (s *Server) ensureModuleVolume(ctx context.Context, key, defaultName string, selection *catalogVolumeSelection) (string, string, error) {
	node, name, err := s.DB.ModuleVolumeSelection(ctx, key)
	registered := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", "", err
	}
	if registered && selection != nil && (selection.Node != node || (selection.Name != "" && selection.Name != name)) {
		return "", "", fmt.Errorf("%w: use the original volume and node", errModuleStorage)
	}
	if !registered && selection != nil {
		node, name = selection.Node, selection.Name
	}
	if node == "" {
		node, err = s.Docker.LocalSwarmNode(ctx)
		if err != nil {
			return "", "", err
		}
	}
	if err := s.checkVolumeNode(ctx, node); err != nil {
		if errdefs.IsUnavailable(err) {
			return "", "", errModuleStorage
		}
		return "", "", err
	}
	create := name == ""
	if name == "" {
		name = defaultName
	}
	_, err = s.nodeVolume(ctx, node, name, false)
	if errdefs.IsNotFound(err) && create {
		_, err = s.nodeVolume(ctx, node, name, true)
		if errdefs.IsConflict(err) || errors.Is(err, docker.ErrNetworkConflict) {
			_, err = s.nodeVolume(ctx, node, name, false)
		}
	}
	if err != nil {
		if errdefs.IsUnavailable(err) || errdefs.IsNotFound(err) {
			return "", "", fmt.Errorf("%w: volume %s on node %s", errModuleStorage, name, node)
		}
		return "", "", err
	}
	return s.DB.ModuleVolume(ctx, key, node, name)
}

func (s *Server) registerVolumes(api huma.API, viewer, admin huma.Middlewares) {
	huma.Register(api, huma.Operation{OperationID: "list-node-volumes", Method: http.MethodGet, Path: volumeRoute, Summary: "List local named volumes on a Swarm node", Middlewares: viewer, Security: signedIn}, func(ctx context.Context, in *volumeNode) (*volumeListOutput, error) {
		rows := []docker.Volume{}
		err := s.withLocalNode(ctx, in.Node, func() error {
			var err error
			rows, err = s.Docker.LocalVolumes(ctx)
			return err
		}, http.MethodGet, "/volumes", nil, &rows)
		if err != nil {
			return nil, volumeProblem(err)
		}
		attachments, err := s.Docker.VolumeAttachments(ctx, in.Node)
		if err != nil {
			return nil, volumeProblem(err)
		}
		for i := range rows {
			setVolumeAttachments(&rows[i], in.Node, attachments)
		}
		return &volumeListOutput{Body: rows}, nil
	})
	for _, create := range []bool{false, true} {
		op := huma.Operation{OperationID: "get-node-volume", Method: http.MethodGet, Path: volumeRoute + "/{name}", Summary: "Inspect a local volume and its service attachments", Middlewares: viewer, Security: signedIn}
		if create {
			op.OperationID, op.Method, op.Summary, op.Middlewares = "create-node-volume", http.MethodPut, "Create a local named volume on a node", admin
		}
		huma.Register(api, op, func(ctx context.Context, in *volumePath) (*volumeOutput, error) {
			if create {
				s.volumeMu.Lock()
				defer s.volumeMu.Unlock()
				if err := s.checkVolumeNode(ctx, in.Node); err != nil {
					return nil, volumeProblem(err)
				}
			}
			volume, err := s.nodeVolume(ctx, in.Node, in.Name, create)
			if err != nil {
				return nil, volumeProblem(err)
			}
			attachments, err := s.Docker.VolumeAttachments(ctx, in.Node)
			if err != nil {
				return nil, volumeProblem(err)
			}
			setVolumeAttachments(&volume, in.Node, attachments)
			return &volumeOutput{Body: volume}, nil
		})
	}
	huma.Register(api, huma.Operation{OperationID: "remove-node-volume", Method: http.MethodDelete, Path: volumeRoute + "/{name}", DefaultStatus: http.StatusNoContent, Summary: "Delete an unused local volume", Middlewares: admin, Security: signedIn}, func(ctx context.Context, in *volumePath) (*struct{}, error) {
		s.volumeMu.Lock()
		defer s.volumeMu.Unlock()
		if err := s.checkVolumeNode(ctx, in.Node); err != nil {
			return nil, volumeProblem(err)
		}
		attachments, err := s.Docker.VolumeAttachments(ctx, in.Node)
		if err != nil {
			return nil, volumeProblem(err)
		}
		if len(attachments[in.Name]) != 0 {
			return nil, volumeProblem(errdefs.ErrConflict)
		}
		err = s.withLocalNode(ctx, in.Node, func() error {
			return s.Docker.RemoveLocalVolume(ctx, in.Name)
		}, http.MethodDelete, "/volumes/"+in.Name, nil, nil)
		if err != nil {
			return nil, volumeProblem(err)
		}
		return nil, nil
	})
	huma.Register(api, huma.Operation{OperationID: "attach-service-volume", Method: http.MethodPost, Path: "/api/v1/services/{id}/volumes", DefaultStatus: http.StatusNoContent, Summary: "Mount an existing local volume and pin the service to its node", Middlewares: admin, Security: signedIn}, func(ctx context.Context, in *serviceVolumeInput) (*struct{}, error) {
		s.volumeMu.Lock()
		defer s.volumeMu.Unlock()
		if err := s.checkVolumeNode(ctx, in.Body.Node); err != nil {
			return nil, volumeProblem(err)
		}
		if _, err := s.nodeVolume(ctx, in.Body.Node, in.Body.Name, false); err != nil {
			return nil, volumeProblem(err)
		}
		if err := s.Docker.AttachServiceVolume(ctx, in.ID, in.Body.Node, in.Body.Name, in.Body.Target, in.Body.ReadOnly); err != nil {
			return nil, volumeProblem(err)
		}
		return nil, nil
	})
}
