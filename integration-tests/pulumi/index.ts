import * as pulumi from "@pulumi/pulumi";
import * as testapi from "./sdk/nodejs";

// Create an organisation
const org = new testapi.organisations.Organisations("test-org", {
  name: "Test Organisation",
});

// Create a team scoped under the organisation
const team = new testapi.teams.OrganisationsTeams("test-team", {
  name: "Engineering",
  organisationId: org.id,
});

// Create a user scoped under the organisation's team
const user = new testapi.users.OrganisationsTeamsUsers("test-user", {
  name: "Alice",
  email: "alice@example.com",
  organisationId: org.id,
  teamId: team.id,
});

export const organisationId = org.id;
export const teamId = team.id;
export const userId = user.id;
