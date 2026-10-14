// The server's rule for stack and service names (docker.ValidName). The form
// checks it first because the name becomes part of the request path.
const NAME = /^[a-zA-Z0-9][a-zA-Z0-9_.-]*$/;

export function validStackName(name: string): boolean {
  return name.length <= 63 && NAME.test(name);
}

export const SAMPLE_COMPOSE = `services:
  web:
    image: nginx:alpine
    deploy:
      replicas: 1
`;
