# Codex project instructions

Read and follow [AGENT.md](AGENT.md). After each completed repository change, run relevant checks, commit on the current branch, and push to its configured remote. Never force-push.

## Development service after changes

- After every completed repository change, start or restart the development Web service as a detached background process listening on `0.0.0.0:8080`. Keep its runtime data and logs under the ignored `.redis-shake-web/dev` directory.
- Preserve active migration tasks. Check for active runs before changing the runner process; restart the Web process alone when tasks are active.
- Verify that the homepage and `/api/bootstrap/status` respond successfully before reporting completion.
- Include the localhost URL and current LAN URL(s) for active network interfaces in the final response. Discover the addresses at the time of the change; do not hardcode this machine's IP addresses in the project configuration.
- If the service cannot start or a LAN address cannot be verified, report the exact blocker instead of claiming it is available.
