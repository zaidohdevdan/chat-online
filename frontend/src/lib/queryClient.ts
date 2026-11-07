import { QueryClient } from '@tanstack/react-query';
import { ApiError } from './http';

export function createAppQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 15_000,
        retry: (failureCount, error) => {
          if (error instanceof ApiError) {
            if ([400, 401, 403, 404, 409, 422].includes(error.status)) {
              return false;
            }
          }
          return failureCount < 2;
        },
        refetchOnWindowFocus: false,
      },
      mutations: {
        retry: false,
      },
    },
  });
}

