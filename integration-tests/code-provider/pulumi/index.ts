import * as testapi from "./sdk/nodejs";

const org = new testapi.Organisations("test-org", {
  name: "Test Organisation",
});

const team = new testapi.OrganisationsTeams("test-team", {
  name: "Engineering",
  organisationId: org.id,
});

const user = new testapi.Users("test-user", {
  name: "Alice",
  email: "alice@example.com",
});

const membership = new testapi.OrganisationsTeamsMembers("test-membership", {
  organisationId: org.id,
  teamId: team.id,
  userId: user.id,
});

export const organisationId = org.id;
export const teamId = team.id;
export const userId = user.id;
export const memberId = membership.id;
