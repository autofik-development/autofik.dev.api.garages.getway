// Prints the garages CMS GraphQL schema (SDL) without booting the CMS - no
// database, Redis or RabbitMQ needed - by building it from the CMS's
// code-first resolvers. Its output is test/testdata/cms.graphql, which
// test/schema_test.go validates every gateway operation against.
//
// Run it with the CMS checkout's own TypeScript toolchain:
//
//   CMS=../../autofik.dev.api.garages.cms
//   NODE_PATH=$CMS/node_modules CMS_DIR=$CMS \
//     $CMS/node_modules/.bin/ts-node -T --project $CMS/tsconfig.json \
//     scripts/print-cms-schema.ts > /tmp/cms.graphql
//
// then replace the schema below the header comment in
// test/testdata/cms.graphql with /tmp/cms.graphql.
import 'reflect-metadata';
import * as path from 'path';
import { NestFactory } from '@nestjs/core';
import {
  GraphQLSchemaBuilderModule,
  GraphQLSchemaFactory,
} from '@nestjs/graphql';
import { printSchema } from 'graphql';

async function main() {
  const cmsDir = process.env.CMS_DIR;
  if (!cmsDir) {
    throw new Error('CMS_DIR must point at the autofik.dev.api.garages.cms checkout');
  }
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const resolvers = require(path.resolve(cmsDir, 'src/resolver'));

  const app = await NestFactory.create(GraphQLSchemaBuilderModule, {
    logger: false,
  });
  await app.init();
  const schema = await app
    .get(GraphQLSchemaFactory)
    .create([
      resolvers.GarageResolver,
      resolvers.GarageServiceResolver,
      resolvers.StaffResolver,
    ]);
  process.stdout.write(printSchema(schema));
  await app.close();
}

void main();
