import { Hono } from "hono";
import { resolver, validator, describeRoute } from "hono-openapi";
import * as v from "valibot";
import { eq } from "drizzle-orm";
import { db } from "../db";
import { invites } from "../db/schema";

const BEARER_TOKEN = "test-bearer-token";

const inviteBody = v.object({
  email: v.pipe(v.string(), v.email()),
});

const inviteResponse = v.object({
  id: v.string(),
  email: v.string(),
  organisationId: v.string(),
  createdAt: v.nullable(v.string()),
});

const errorResponse = v.object({ error: v.string() });

const authMiddleware = async (c: any, next: any) => {
  const auth = c.req.header("Authorization");
  if (auth !== `bearer ${BEARER_TOKEN}`) {
    return c.json({ error: "Unauthorized" }, 401);
  }
  await next();
};

export const invitesRouter = new Hono();

invitesRouter.post(
  "/",
  authMiddleware,
  describeRoute({
    tags: ["invites"],
    summary: "Create an invite",
    security: [{ BearerAuth: [] }],
    responses: {
      201: {
        description: "Invite created",
        content: {
          "application/json": { schema: resolver(inviteResponse) },
        },
      },
    },
  }),
  validator("json", inviteBody),
  async (c) => {
    const { organisationId } = c.req.param();
    const body = c.req.valid("json");
    const [invite] = await db
      .insert(invites)
      .values({ email: body.email, organisationId })
      .returning();
    return c.json(
      { ...invite, createdAt: invite.createdAt?.toISOString() ?? null },
      201,
    );
  },
);

invitesRouter.get(
  "/:inviteId",
  authMiddleware,
  describeRoute({
    tags: ["invites"],
    summary: "Get an invite",
    security: [{ BearerAuth: [] }],
    responses: {
      200: {
        description: "Invite found",
        content: {
          "application/json": { schema: resolver(inviteResponse) },
        },
      },
      404: {
        description: "Not found",
        content: { "application/json": { schema: resolver(errorResponse) } },
      },
    },
  }),
  async (c) => {
    const { inviteId } = c.req.param();
    const invite = await db.query.invites.findFirst({
      where: eq(invites.id, inviteId),
    });
    if (!invite) return c.json({ error: "Invite not found" }, 404);
    return c.json({
      ...invite,
      createdAt: invite.createdAt?.toISOString() ?? null,
    });
  },
);

invitesRouter.delete(
  "/:inviteId",
  authMiddleware,
  describeRoute({
    tags: ["invites"],
    summary: "Delete an invite",
    security: [{ BearerAuth: [] }],
    responses: {
      204: { description: "Deleted" },
      404: {
        description: "Not found",
        content: { "application/json": { schema: resolver(errorResponse) } },
      },
    },
  }),
  async (c) => {
    const { inviteId } = c.req.param();
    const result = await db
      .delete(invites)
      .where(eq(invites.id, inviteId))
      .returning();
    if (result.length === 0) return c.json({ error: "Invite not found" }, 404);
    return new Response(null, { status: 204 });
  },
);
