import * as api from "@pulumi/integration-test-api";

const org = new api.Organisations("test-org", {
  name: "Test Organisation",
});

const team = new api.OrganisationsTeams("test-team", {
  name: "Engineering",
  organisationId: org.id,
});

const user = new api.Users("test-user", {
  name: "Alice",
  email: "alice@example.com",
});

const membership = new api.OrganisationsTeamsMembers("test-membership", {
  organisationId: org.id,
  teamId: team.id,
  userId: user.id,
});

const invite = new api.OrganisationsInvites("test-invite", {
  organisationId: org.id,
  email: "bob@example.com",
});

export const organisationId = org.id;
export const teamId = team.id;
export const userId = user.id;
export const memberId = membership.id;
export const inviteId = invite.id;
