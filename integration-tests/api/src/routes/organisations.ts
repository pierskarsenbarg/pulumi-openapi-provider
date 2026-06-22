import { Hono } from "hono";
import { describeRoute } from "hono-openapi";
import { resolver, validator } from "hono-openapi/valibot";
import * as v from "valibot";
import { eq } from "drizzle-orm";
import { db } from "../db";
import { organisations } from "../db/schema";

const organisationBody = v.object({
  name: v.pipe(v.string(), v.minLength(1)),
});

const organisationResponse = v.object({
  id: v.string(),
  name: v.string(),
  createdAt: v.nullable(v.string()),
});

const errorResponse = v.object({ error: v.string() });

export const organisationsRouter = new Hono();

organisationsRouter.post(
  "/",
  describeRoute({
    tags: ["organisations"],
    summary: "Create an organisation",
    responses: {
      201: {
        description: "Organisation created",
        content: { "application/json": { schema: resolver(organisationResponse) } },
      },
    },
  }),
  validator("json", organisationBody),
  async (c) => {
    const body = c.req.valid("json");
    const [org] = await db.insert(organisations).values({ name: body.name }).returning();
    return c.json({ ...org, createdAt: org.createdAt?.toISOString() ?? null }, 201);
  }
);

organisationsRouter.get(
  "/:organisationId",
  describeRoute({
    tags: ["organisations"],
    summary: "Get an organisation",
    responses: {
      200: {
        description: "Organisation found",
        content: { "application/json": { schema: resolver(organisationResponse) } },
      },
      404: {
        description: "Not found",
        content: { "application/json": { schema: resolver(errorResponse) } },
      },
    },
  }),
  async (c) => {
    const { organisationId } = c.req.param();
    const org = await db.query.organisations.findFirst({
      where: eq(organisations.id, organisationId),
    });
    if (!org) return c.json({ error: "Organisation not found" }, 404);
    return c.json({ ...org, createdAt: org.createdAt?.toISOString() ?? null });
  }
);

organisationsRouter.patch(
  "/:organisationId",
  describeRoute({
    tags: ["organisations"],
    summary: "Update an organisation",
    responses: {
      200: {
        description: "Organisation updated",
        content: { "application/json": { schema: resolver(organisationResponse) } },
      },
      404: {
        description: "Not found",
        content: { "application/json": { schema: resolver(errorResponse) } },
      },
    },
  }),
  validator("json", organisationBody),
  async (c) => {
    const { organisationId } = c.req.param();
    const body = c.req.valid("json");
    const [org] = await db
      .update(organisations)
      .set({ name: body.name })
      .where(eq(organisations.id, organisationId))
      .returning();
    if (!org) return c.json({ error: "Organisation not found" }, 404);
    return c.json({ ...org, createdAt: org.createdAt?.toISOString() ?? null });
  }
);

organisationsRouter.delete(
  "/:organisationId",
  describeRoute({
    tags: ["organisations"],
    summary: "Delete an organisation",
    responses: {
      204: { description: "Deleted" },
      404: {
        description: "Not found",
        content: { "application/json": { schema: resolver(errorResponse) } },
      },
    },
  }),
  async (c) => {
    const { organisationId } = c.req.param();
    const result = await db
      .delete(organisations)
      .where(eq(organisations.id, organisationId))
      .returning();
    if (result.length === 0) return c.json({ error: "Organisation not found" }, 404);
    return new Response(null, { status: 204 });
  }
);
