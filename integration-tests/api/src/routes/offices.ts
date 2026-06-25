import { Hono } from "hono";
import { validator } from "hono-openapi";
import * as v from "valibot";
import { eq } from "drizzle-orm";
import { db } from "../db";
import { offices } from "../db/schema";

const officeBody = v.object({
  name: v.pipe(v.string(), v.minLength(1)),
  location: v.optional(v.string()),
});

export const officesRouter = new Hono();

officesRouter.post("/", validator("json", officeBody), async (c) => {
  const body = c.req.valid("json");
  const [office] = await db
    .insert(offices)
    .values({ name: body.name, location: body.location ?? null })
    .returning();
  return c.json(
    { ...office, createdAt: office.createdAt?.toISOString() ?? null },
    201,
  );
});

officesRouter.get("/:officeId", async (c) => {
  const { officeId } = c.req.param();
  const office = await db.query.offices.findFirst({
    where: eq(offices.id, officeId),
  });
  if (!office) return c.json({ error: "Office not found" }, 404);
  return c.json({
    ...office,
    createdAt: office.createdAt?.toISOString() ?? null,
  });
});

officesRouter.delete("/:officeId", async (c) => {
  const { officeId } = c.req.param();
  const result = await db
    .delete(offices)
    .where(eq(offices.id, officeId))
    .returning();
  if (result.length === 0) return c.json({ error: "Office not found" }, 404);
  return new Response(null, { status: 204 });
});
