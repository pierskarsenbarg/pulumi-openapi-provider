import { Hono } from "hono";
import { resolver, validator, describeRoute } from "hono-openapi";
import * as v from "valibot";
import { and, eq } from "drizzle-orm";
import { db } from "../db";
import { organisations, teams } from "../db/schema";

const teamBody = v.object({
  name: v.pipe(v.string(), v.minLength(1)),
});

const teamResponse = v.object({
  id: v.string(),
  name: v.string(),
  organisationId: v.string(),
  createdAt: v.nullable(v.string()),
});

const errorResponse = v.object({ error: v.string() });

export const teamsRouter = new Hono<{ Variables: Record<string, string> }>();

teamsRouter.post(
  "/",
  describeRoute({
    tags: ["teams"],
    summary: "Create a team within an organisation",
    responses: {
      201: {
        description: "Team created",
        content: { "application/json": { schema: resolver(teamResponse) } },
      },
      404: {
        description: "Organisation not found",
        content: { "application/json": { schema: resolver(errorResponse) } },
      },
    },
  }),
  validator("json", teamBody),
  async (c) => {
    const { organisationId } = c.req.param();
    const body = c.req.valid("json");

    const org = await db.query.organisations.findFirst({
      where: eq(organisations.id, organisationId),
    });
    if (!org) return c.json({ error: "Organisation not found" }, 404);

    const [team] = await db
      .insert(teams)
      .values({ name: body.name, organisationId })
      .returning();
    return c.json(
      { ...team, createdAt: team.createdAt?.toISOString() ?? null },
      201,
    );
  },
);

teamsRouter.get(
  "/:teamId",
  describeRoute({
    tags: ["teams"],
    summary: "Get a team",
    responses: {
      200: {
        description: "Team found",
        content: { "application/json": { schema: resolver(teamResponse) } },
      },
      404: {
        description: "Not found",
        content: { "application/json": { schema: resolver(errorResponse) } },
      },
    },
  }),
  async (c) => {
    const { organisationId, teamId } = c.req.param();
    const team = await db.query.teams.findFirst({
      where: and(
        eq(teams.id, teamId),
        eq(teams.organisationId, organisationId),
      ),
    });
    if (!team) return c.json({ error: "Team not found" }, 404);
    return c.json({
      ...team,
      createdAt: team.createdAt?.toISOString() ?? null,
    });
  },
);

teamsRouter.patch(
  "/:teamId",
  describeRoute({
    tags: ["teams"],
    summary: "Update a team",
    responses: {
      200: {
        description: "Team updated",
        content: { "application/json": { schema: resolver(teamResponse) } },
      },
      404: {
        description: "Not found",
        content: { "application/json": { schema: resolver(errorResponse) } },
      },
    },
  }),
  validator("json", teamBody),
  async (c) => {
    const { organisationId, teamId } = c.req.param();
    const body = c.req.valid("json");
    const [team] = await db
      .update(teams)
      .set({ name: body.name })
      .where(
        and(eq(teams.id, teamId), eq(teams.organisationId, organisationId)),
      )
      .returning();
    if (!team) return c.json({ error: "Team not found" }, 404);
    return c.json({
      ...team,
      createdAt: team.createdAt?.toISOString() ?? null,
    });
  },
);

teamsRouter.delete(
  "/:teamId",
  describeRoute({
    tags: ["teams"],
    summary: "Delete a team",
    responses: {
      204: { description: "Deleted" },
      404: {
        description: "Not found",
        content: { "application/json": { schema: resolver(errorResponse) } },
      },
    },
  }),
  async (c) => {
    const { organisationId, teamId } = c.req.param();
    const result = await db
      .delete(teams)
      .where(
        and(eq(teams.id, teamId), eq(teams.organisationId, organisationId)),
      )
      .returning();
    if (result.length === 0) return c.json({ error: "Team not found" }, 404);
    return new Response(null, { status: 204 });
  },
);
