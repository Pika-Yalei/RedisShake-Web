# Codex project instructions

## Delivering changes

- After completing any change to this repository, run the checks relevant to that change, commit it on the current branch, and push the commit to the branch's configured remote before reporting completion. This is standing authorization for routine project pushes.
- Do not create or switch branches or use worktrees solely to perform this workflow. Never force-push.
- Keep local runtime data, credentials, secrets, and build outputs out of commits.
- If a push fails, report the commit and the specific blocker; do not claim the change is on the remote.

## UI design

For every UI change, read and follow [docs/ui-design.md](docs/ui-design.md). Keep the page title hierarchy consistent when adding management pages or changing routes.

## Development service after changes

- After every completed repository change, start or restart the development Web service as a detached background process listening on `0.0.0.0:8080`. Keep its runtime data and logs under the ignored `.redis-shake-web/dev` directory.
- Preserve active migration tasks. Check for active runs before changing the runner process; restart the Web process alone when tasks are active.
- Verify that the homepage and `/api/bootstrap/status` respond successfully before reporting completion.
- Include the localhost URL and current LAN URL(s) for active network interfaces in the final response. Discover the addresses at the time of the change; do not hardcode this machine's IP addresses in the project configuration.
- If the service cannot start or a LAN address cannot be verified, report the exact blocker instead of claiming it is available.
