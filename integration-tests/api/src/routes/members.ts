import { Hono } from "hono";
import { describeRoute } from "hono-openapi";
import { resolver, validator } from "hono-openapi/valibot";
import * as v from "valibot";
import { and, eq } from "drizzle-orm";
import { db } from "../db";
import { teamMembers, teams } from "../db/schema";

const memberBody = v.object({
  userId: v.pipe(v.string(), v.minLength(1)),
});

const memberResponse = v.object({
  id: v.string(),
  userId: v.string(),
  teamId: v.string(),
  createdAt: v.nullable(v.string()),
});

const errorResponse = v.object({ error: v.string() });

export const membersRouter = new Hono();

membersRouter.post(
  "/:teamId/members",
  describeRoute({
    tags: ["members"],
    summary: "Add a user to a team",
    responses: {
      201: {
        description: "Member added",
        content: { "application/json": { schema: resolver(memberResponse) } },
      },
      404: {
        description: "Team not found",
        content: { "application/json": { schema: resolver(errorResponse) } },
      },
    },
  }),
  validator("json", memberBody),
  async (c) => {
    const { organisationId, teamId } = c.req.param();
    const body = c.req.valid("json");

    const team = await db.query.teams.findFirst({
      where: and(eq(teams.id, teamId), eq(teams.organisationId, organisationId)),
    });
    if (!team) return c.json({ error: "Team not found" }, 404);

    const [member] = await db
      .insert(teamMembers)
      .values({ userId: body.userId, teamId })
      .returning();
    return c.json({ ...member, createdAt: member.createdAt?.toISOString() ?? null }, 201);
  }
);

membersRouter.get(
  "/:teamId/members/:memberId",
  describeRoute({
    tags: ["members"],
    summary: "Get a team membership",
    responses: {
      200: {
        description: "Membership found",
        content: { "application/json": { schema: resolver(memberResponse) } },
      },
      404: {
        description: "Not found",
        content: { "application/json": { schema: resolver(errorResponse) } },
      },
    },
  }),
  async (c) => {
    const { teamId, memberId } = c.req.param();
    const member = await db.query.teamMembers.findFirst({
      where: and(eq(teamMembers.id, memberId), eq(teamMembers.teamId, teamId)),
    });
    if (!member) return c.json({ error: "Membership not found" }, 404);
    return c.json({ ...member, createdAt: member.createdAt?.toISOString() ?? null });
  }
);

membersRouter.delete(
  "/:teamId/members/:memberId",
  describeRoute({
    tags: ["members"],
    summary: "Remove a user from a team",
    responses: {
      204: { description: "Removed" },
      404: {
        description: "Not found",
        content: { "application/json": { schema: resolver(errorResponse) } },
      },
    },
  }),
  async (c) => {
    const { teamId, memberId } = c.req.param();
    const result = await db
      .delete(teamMembers)
      .where(and(eq(teamMembers.id, memberId), eq(teamMembers.teamId, teamId)))
      .returning();
    if (result.length === 0) return c.json({ error: "Membership not found" }, 404);
    return new Response(null, { status: 204 });
  }
);
