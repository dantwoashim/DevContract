import type { Env } from '../types';
import { handleTeamCoordinatorRequest } from './teamControlPlane';

export class TeamCoordinator {
    constructor(private readonly state: DurableObjectState, private readonly env: Env) {}

    async fetch(request: Request): Promise<Response> {
        return handleTeamCoordinatorRequest(this.state, this.env, request);
    }
}
