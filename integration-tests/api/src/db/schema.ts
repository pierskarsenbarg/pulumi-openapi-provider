import { integer, sqliteTable, text, uniqueIndex } from "drizzle-orm/sqlite-core";

export const organisations = sqliteTable("organisations", {
  id: text("id")
    .primaryKey()
    .$defaultFn(() => crypto.randomUUID()),
  name: text("name").notNull().unique(),
  createdAt: integer("created_at", { mode: "timestamp" }).$defaultFn(
    () => new Date()
  ),
});

export const teams = sqliteTable("teams", {
  id: text("id")
    .primaryKey()
    .$defaultFn(() => crypto.randomUUID()),
  name: text("name").notNull(),
  organisationId: text("organisation_id")
    .notNull()
    .references(() => organisations.id, { onDelete: "cascade" }),
  createdAt: integer("created_at", { mode: "timestamp" }).$defaultFn(
    () => new Date()
  ),
});

export const users = sqliteTable("users", {
  id: text("id")
    .primaryKey()
    .$defaultFn(() => crypto.randomUUID()),
  name: text("name").notNull(),
  email: text("email").notNull().unique(),
  createdAt: integer("created_at", { mode: "timestamp" }).$defaultFn(
    () => new Date()
  ),
});

export const teamMembers = sqliteTable(
  "team_members",
  {
    id: text("id")
      .primaryKey()
      .$defaultFn(() => crypto.randomUUID()),
    userId: text("user_id")
      .notNull()
      .references(() => users.id, { onDelete: "cascade" }),
    teamId: text("team_id")
      .notNull()
      .references(() => teams.id, { onDelete: "cascade" }),
    createdAt: integer("created_at", { mode: "timestamp" }).$defaultFn(
      () => new Date()
    ),
  },
  (t) => [uniqueIndex("team_members_user_team_unique").on(t.userId, t.teamId)]
);

export const invites = sqliteTable("invites", {
  id: text("id")
    .primaryKey()
    .$defaultFn(() => crypto.randomUUID()),
  email: text("email").notNull(),
  organisationId: text("organisation_id")
    .notNull()
    .references(() => organisations.id, { onDelete: "cascade" }),
  createdAt: integer("created_at", { mode: "timestamp" }).$defaultFn(
    () => new Date()
  ),
});

export const offices = sqliteTable("offices", {
  id: text("id")
    .primaryKey()
    .$defaultFn(() => crypto.randomUUID()),
  name: text("name").notNull(),
  location: text("location"),
  createdAt: integer("created_at", { mode: "timestamp" }).$defaultFn(
    () => new Date()
  ),
});

export type Organisation = typeof organisations.$inferSelect;
export type NewOrganisation = typeof organisations.$inferInsert;
export type Team = typeof teams.$inferSelect;
export type NewTeam = typeof teams.$inferInsert;
export type User = typeof users.$inferSelect;
export type NewUser = typeof users.$inferInsert;
export type TeamMember = typeof teamMembers.$inferSelect;
export type NewTeamMember = typeof teamMembers.$inferInsert;
export type Invite = typeof invites.$inferSelect;
export type NewInvite = typeof invites.$inferInsert;
export type Office = typeof offices.$inferSelect;
export type NewOffice = typeof offices.$inferInsert;
