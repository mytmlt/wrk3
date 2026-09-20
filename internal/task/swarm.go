package task

import "fmt"

// swarmTarget is the "swarm" environment: a `docker stack deploy` file.
type swarmTarget struct{}

func (swarmTarget) Name() string { return "swarm" }

func (swarmTarget) Render(d *Definition) ([]byte, error) { return d.Swarm() }

func init() { RegisterTarget(swarmTarget{}) }

// Swarm renders the Definition as a docker swarm stack file. Swarm pulls
// prebuilt images, so services that only define a build context are
// rejected with a pointer to build/push first.
func (d *Definition) Swarm() ([]byte, error) {
	return renderCompose(d, true)
}

// validateSwarm rejects definitions swarm cannot run: build-only services
// (no image) because `docker stack deploy` never builds.
func validateSwarm(d *Definition) error {
	for _, name := range d.ServiceNames() {
		svc := d.Services[name]
		if svc.Image == "" {
			return fmt.Errorf("swarm target: service %q has no image (build contexts are not supported by `docker stack deploy`; build and push the image first)", name)
		}
	}
	return nil
}

// swarmDeploy normalizes the deploy block for a stack file: replicated
// with one replica by default, and no replicas for global mode.
func swarmDeploy(d *Deploy) *composeDeployOut {
	out := &composeDeployOut{}
	if d != nil {
		out.Replicas = d.Replicas
		out.Mode = d.Mode
	}
	if out.Mode == DeployModeGlobal {
		out.Replicas = 0
		return out
	}
	if out.Mode == "" {
		out.Mode = DeployModeReplicated
	}
	if out.Replicas == 0 {
		out.Replicas = 1
	}
	return out
}
