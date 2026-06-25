import { Hono } from "hono";
import { openAPIRouteHandler } from "hono-openapi";
import { organisationsRouter } from "./routes/organisations";
import { teamsRouter } from "./routes/teams";
import { usersRouter } from "./routes/users";
import { membersRouter } from "./routes/members";
import { officesRouter } from "./routes/offices";

const app = new Hono();

app.route("/users", usersRouter);
app.route("/organisations", organisationsRouter);
app.route("/organisations/:organisationId/teams", teamsRouter);
app.route("/organisations/:organisationId/teams", membersRouter);
app.route("/offices", officesRouter);

app.get(
  "/openapi",
  openAPIRouteHandler(app, {
    documentation: {
      info: {
        title: "Integration Test API",
        version: "1.0.0",
        description: "API for pulumi-openapi-provider integration tests",
      },
      servers: [{ url: `http://localhost:${process.env.PORT ?? 3000}` }],
    },
  }),
);

const port = Number(process.env.PORT ?? 3000);
console.log(`API listening on http://localhost:${port}`);
console.log(`OpenAPI spec at http://localhost:${port}/openapi`);

export default { port, fetch: app.fetch };
