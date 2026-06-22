import { Hono } from "hono";
import { resolver, validator, describeRoute } from "hono-openapi";
import * as v from "valibot";
import { eq } from "drizzle-orm";
import { db } from "../db";
import { users } from "../db/schema";

const userBody = v.object({
  name: v.pipe(v.string(), v.minLength(1)),
  email: v.pipe(v.string(), v.email()),
});

const userResponse = v.object({
  id: v.string(),
  name: v.string(),
  email: v.string(),
  createdAt: v.nullable(v.string()),
});

const errorResponse = v.object({ error: v.string() });

export const usersRouter = new Hono();

usersRouter.post(
  "/",
  describeRoute({
    tags: ["users"],
    summary: "Create a user",
    responses: {
      201: {
        description: "User created",
        content: { "application/json": { schema: resolver(userResponse) } },
      },
    },
  }),
  validator("json", userBody),
  async (c) => {
    const body = c.req.valid("json");
    const [user] = await db
      .insert(users)
      .values({ name: body.name, email: body.email })
      .returning();
    return c.json(
      { ...user, createdAt: user.createdAt?.toISOString() ?? null },
      201,
    );
  },
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
    const { userId } = c.req.param();
    const user = await db.query.users.findFirst({
      where: eq(users.id, userId),
    });
    if (!user) return c.json({ error: "User not found" }, 404);
    return c.json({
      ...user,
      createdAt: user.createdAt?.toISOString() ?? null,
    });
  },
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
    const { userId } = c.req.param();
    const body = c.req.valid("json");
    const [user] = await db
      .update(users)
      .set({ name: body.name, email: body.email })
      .where(eq(users.id, userId))
      .returning();
    if (!user) return c.json({ error: "User not found" }, 404);
    return c.json({
      ...user,
      createdAt: user.createdAt?.toISOString() ?? null,
    });
  },
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
    const { userId } = c.req.param();
    const result = await db
      .delete(users)
      .where(eq(users.id, userId))
      .returning();
    if (result.length === 0) return c.json({ error: "User not found" }, 404);
    return new Response(null, { status: 204 });
  },
);
