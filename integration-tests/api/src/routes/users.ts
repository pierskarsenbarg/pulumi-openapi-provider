import { Hono } from "hono";
import { describeRoute } from "hono-openapi";
import { resolver, validator } from "hono-openapi/valibot";
import * as v from "valibot";
import { and, eq } from "drizzle-orm";
import { db } from "../db";
import { teams, users } from "../db/schema";

const userBody = v.object({
  name: v.pipe(v.string(), v.minLength(1)),
  email: v.pipe(v.string(), v.email()),
});

const userResponse = v.object({
  id: v.string(),
  name: v.string(),
  email: v.string(),
  teamId: v.string(),
  createdAt: v.nullable(v.string()),
});

const errorResponse = v.object({ error: v.string() });

export const usersRouter = new Hono();

usersRouter.post(
  "/",
  describeRoute({
    tags: ["users"],
    summary: "Create a user within a team",
    responses: {
      201: {
        description: "User created",
        content: { "application/json": { schema: resolver(userResponse) } },
      },
      404: {
        description: "Team not found",
        content: { "application/json": { schema: resolver(errorResponse) } },
      },
    },
  }),
  validator("json", userBody),
  async (c) => {
    const { organisationId, teamId } = c.req.param();
    const body = c.req.valid("json");

    const team = await db.query.teams.findFirst({
      where: and(eq(teams.id, teamId), eq(teams.organisationId, organisationId)),
    });
    if (!team) return c.json({ error: "Team not found" }, 404);

    const [user] = await db
      .insert(users)
      .values({ name: body.name, email: body.email, teamId })
      .returning();
    return c.json({ ...user, createdAt: user.createdAt?.toISOString() ?? null }, 201);
  }
);

usersRouter.get(
  "/:userId",
  describeRoute({
    tags: ["users"],
    summary: "Get a user",
    responses: {
      200: {
        description: "User found",
        content: { "application/json": { schema: resolver(userResponse) } },
      },
      404: {
        description: "Not found",
        content: { "application/json": { schema: resolver(errorResponse) } },
      },
    },
  }),
  async (c) => {
    const { teamId, userId } = c.req.param();
    const user = await db.query.users.findFirst({
      where: and(eq(users.id, userId), eq(users.teamId, teamId)),
    });
    if (!user) return c.json({ error: "User not found" }, 404);
    return c.json({ ...user, createdAt: user.createdAt?.toISOString() ?? null });
  }
);

usersRouter.patch(
  "/:userId",
  describeRoute({
    tags: ["users"],
    summary: "Update a user",
    responses: {
      200: {
        description: "User updated",
        content: { "application/json": { schema: resolver(userResponse) } },
      },
      404: {
        description: "Not found",
        content: { "application/json": { schema: resolver(errorResponse) } },
      },
    },
  }),
  validator("json", userBody),
  async (c) => {
    const { teamId, userId } = c.req.param();
    const body = c.req.valid("json");
    const [user] = await db
      .update(users)
      .set({ name: body.name, email: body.email })
      .where(and(eq(users.id, userId), eq(users.teamId, teamId)))
      .returning();
    if (!user) return c.json({ error: "User not found" }, 404);
    return c.json({ ...user, createdAt: user.createdAt?.toISOString() ?? null });
  }
);

usersRouter.delete(
  "/:userId",
  describeRoute({
    tags: ["users"],
    summary: "Delete a user",
    responses: {
      204: { description: "Deleted" },
      404: {
        description: "Not found",
        content: { "application/json": { schema: resolver(errorResponse) } },
      },
    },
  }),
  async (c) => {
    const { teamId, userId } = c.req.param();
    const result = await db
      .delete(users)
      .where(and(eq(users.id, userId), eq(users.teamId, teamId)))
      .returning();
    if (result.length === 0) return c.json({ error: "User not found" }, 404);
    return new Response(null, { status: 204 });
  }
);
